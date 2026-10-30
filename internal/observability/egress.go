package observability

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/kyledickey/shed/internal/host"
	"github.com/kyledickey/shed/internal/telemetry"
	"github.com/kyledickey/shed/internal/victoria"
)

// Bounds of the egress worker.
const (
	// seenWindow is how long a destination stays known without being
	// reached again.
	seenWindow = 7 * 24 * time.Hour
	// maxSeen bounds the destinations remembered.
	maxSeen = 1 << 16
)

// dest is where a service's connections go.
type dest struct {
	service string
	addr    netip.Addr
	port    uint16
	proto   uint8
}

// usage is what the connections of a service to one destination moved.
type usage struct {
	// flows counts the connections that moved packets or began since the
	// previous dump.
	flows int
	// out is what the service sent, in what its destination answered.
	out, in host.Counters
}

// flowKey identifies a connection between dumps.
type flowKey struct {
	proto    uint8
	src, dst host.Endpoint
}

// tracker turns successive dumps of the conntrack table, whose counters are
// totals, into the traffic of each interval.
type tracker struct {
	prev map[flowKey][2]host.Counters
	// seeded reports whether a dump has been taken. The connections in the
	// first one began before the worker did, so their totals are not an
	// interval's traffic.
	seeded bool
}

// update returns the usage of every destination the connections in flows
// reach, for the connections whose original source owner names a service
// and whose destination is not internal. A destination is present even when
// its usage is empty.
func (t *tracker) update(flows []host.Flow, owner func(netip.Addr) (string, bool), internal func(netip.Addr) bool) map[dest]*usage {
	out := make(map[dest]*usage)
	next := make(map[flowKey][2]host.Counters)
	for _, f := range flows {
		svc, ok := owner(f.Orig.Src.Addr)
		if !ok || !f.Orig.Dst.Addr.IsValid() || f.Orig.Dst.Addr.IsLoopback() || f.Orig.Dst.Addr.IsUnspecified() || internal(f.Orig.Dst.Addr) {
			continue
		}
		d := dest{svc, f.Orig.Dst.Addr.Unmap(), f.Orig.Dst.Port, f.Proto}
		u := out[d]
		if u == nil {
			u = &usage{}
			out[d] = u
		}
		key := flowKey{f.Proto, f.Orig.Src, f.Orig.Dst}
		cur := [2]host.Counters{f.OrigCounters, f.ReplyCounters}
		next[key] = cur
		if !t.seeded {
			continue
		}
		prev, known := t.prev[key]
		// A counter below its previous reading belongs to a new connection
		// with the same addresses.
		if known && (cur[0].Packets < prev[0].Packets || cur[0].Bytes < prev[0].Bytes) {
			known, prev = false, [2]host.Counters{}
		}
		d0, d1 := sub(cur[0], prev[0]), sub(cur[1], prev[1])
		if !known || d0.Packets > 0 || d1.Packets > 0 {
			u.flows++
			u.out.Packets += d0.Packets
			u.out.Bytes += d0.Bytes
			u.in.Packets += d1.Packets
			u.in.Bytes += d1.Bytes
		}
	}
	t.prev, t.seeded = next, true
	return out
}

func sub(a, b host.Counters) host.Counters {
	return host.Counters{Packets: a.Packets - min(a.Packets, b.Packets), Bytes: a.Bytes - min(a.Bytes, b.Bytes)}
}

// seenKey is a destination of a service, whatever the protocol.
type seenKey struct {
	service string
	addr    netip.Addr
	port    uint16
}

// seenSet remembers the destinations services have reached.
type seenSet struct {
	at map[seenKey]time.Time
}

// observe records that key was reached at now and reports whether it had
// not been in the last [seenWindow]. Beyond [maxSeen] it forgets the
// longest unreached destination.
func (s *seenSet) observe(key seenKey, now time.Time) bool {
	if s.at == nil {
		s.at = make(map[seenKey]time.Time)
	}
	last, known := s.at[key]
	if !known && len(s.at) >= maxSeen {
		var oldest seenKey
		var oldestAt time.Time
		for k, t := range s.at {
			if oldestAt.IsZero() || t.Before(oldestAt) {
				oldest, oldestAt = k, t
			}
		}
		delete(s.at, oldest)
	}
	s.at[key] = now
	return !known || now.Sub(last) > seenWindow
}

// protoName returns the name of the IP protocol number p.
func protoName(p uint8) string {
	switch p {
	case 1:
		return "icmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 58:
		return "icmpv6"
	}
	return strconv.Itoa(int(p))
}

// seedQuery finds the destinations the egress records of the last
// [seenWindow] name.
const seedQuery = `{shed.source="egress"} | stats by (shed.service.id, dst, dst_port) count() n`

// runEgress records the connections containers make to the outside while
// telemetry is on, every interval, until ctx is done.
func (s *security) runEgress(ctx context.Context) {
	if err := s.Conntrack.EnableAccounting(); err != nil {
		s.log.Warn("enable conntrack accounting: egress bytes stay zero", "err", err)
	}
	var seen seenSet
	s.seed(ctx, &seen)
	var t tracker
	var lastErr string
	for {
		if err := s.egress(ctx, &t, &seen); err != nil && ctx.Err() == nil {
			// The same failure, such as a missing module, is logged once.
			if msg := err.Error(); msg != lastErr {
				s.log.Warn("record egress", "err", err)
				lastErr = msg
			}
		} else {
			lastErr = ""
		}
		if !sleep(ctx, s.interval) {
			return
		}
	}
}

// seed fills seen from the egress records of the last seven days.
func (s *security) seed(ctx context.Context, seen *seenSet) {
	now := s.now()
	rows, err := s.Logs.Query(ctx, seedQuery, now.Add(-seenWindow), time.Time{}, 0)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("seed egress destinations: new ones are reported again", "err", err)
		}
		return
	}
	for _, r := range rows {
		addr, aerr := netip.ParseAddr(r["dst"])
		port, perr := strconv.ParseUint(r["dst_port"], 10, 16)
		if aerr == nil && perr == nil {
			seen.observe(seenKey{r["shed.service.id"], addr, uint16(port)}, now)
		}
	}
}

// egress takes one dump of the conntrack table and records the traffic since
// the previous one.
func (s *security) egress(ctx context.Context, t *tracker, seen *seenSet) error {
	ctrs, err := s.Docker.List(ctx, map[string]string{labelService: ""})
	if err != nil {
		return err
	}
	subnets, err := s.Docker.NetworkSubnets(ctx)
	if err != nil {
		return err
	}
	flows, err := s.Conntrack.Flows(ctx)
	if err != nil {
		return err
	}
	now := s.now()

	// The service of each container address, and the labels naming it.
	owners := make(map[netip.Addr]string)
	labels := make(map[string]map[string]string)
	for _, c := range ctrs {
		id := c.Labels[labelService]
		labels[id] = c.Labels
		for _, ip := range c.IPs {
			if a, err := netip.ParseAddr(ip); err == nil {
				owners[a] = id
			}
		}
	}
	owner := func(a netip.Addr) (string, bool) {
		id, ok := owners[a.Unmap()]
		return id, ok
	}
	internal := func(a netip.Addr) bool {
		return slices.ContainsFunc(subnets, func(p netip.Prefix) bool { return p.Contains(a) })
	}

	var records []victoria.Record
	services := make(map[string]telemetry.Service)
	for d, u := range t.update(flows, owner, internal) {
		svc, ok := services[d.service]
		if !ok {
			svc = s.service(ctx, labels[d.service])
			services[d.service] = svc
		}
		to := netip.AddrPortFrom(d.addr, d.port).String()
		if u.flows > 0 {
			r := record(now, svc, "egress", "info", fmt.Sprintf("egress %s %s", protoName(d.proto), to))
			setDest(r, d)
			r["flows"] = strconv.Itoa(u.flows)
			r["packets"], r["bytes"] = strconv.FormatUint(u.out.Packets, 10), strconv.FormatUint(u.out.Bytes, 10)
			r["packets_in"], r["bytes_in"] = strconv.FormatUint(u.in.Packets, 10), strconv.FormatUint(u.in.Bytes, 10)
			records = append(records, r)
			s.egressBytes.Add(ctx, int64(u.out.Bytes), metric.WithAttributes(
				append(svc.Labels(), attribute.String("proto", protoName(d.proto)))...))
		}
		if seen.observe(seenKey{d.service, d.addr, d.port}, now) {
			r := record(now, svc, "egress", "warn", "egress.first_seen "+to)
			setDest(r, d)
			r["event"] = "egress.first_seen"
			records = append(records, r)
		}
	}
	s.ship(ctx, records...)
	return nil
}

func setDest(r victoria.Record, d dest) {
	r["dst"], r["dst_port"], r["proto"] = d.addr.String(), strconv.Itoa(int(d.port)), protoName(d.proto)
}
