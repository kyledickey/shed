package otlp

import (
	"cmp"
	"encoding/binary"
	"hash/maphash"
	"math"
	"slices"
	"strings"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

const serviceNameKey = "service.name"

// shedKeys are the resource attributes shed owns; senders' values are
// replaced.
var shedKeys = map[string]bool{
	"shed.project.id":    true,
	"shed.project.name":  true,
	"shed.service.id":    true,
	"shed.service.name":  true,
	"shed.service.kind":  true,
	"shed.deployment.id": true,
	"shed.source":        true,
}

// stamp masks res's attributes and sets the shed.* attributes from src,
// keeping a non-empty service.name the sender set. It returns res, or a new
// resource if res is nil.
func stamp(res *resourcepb.Resource, src Source, mask func(string) string) *resourcepb.Resource {
	if res == nil {
		res = &resourcepb.Resource{}
	}
	maskAttrs(res.Attributes, mask)
	hasName := false
	kept := res.Attributes[:0]
	for _, kv := range res.Attributes {
		if shedKeys[kv.GetKey()] {
			continue
		}
		if kv.GetKey() == serviceNameKey {
			if kv.GetValue().GetStringValue() == "" {
				continue
			}
			hasName = true
		}
		kept = append(kept, kv)
	}
	origin := cmp.Or(src.Origin, "otlp")
	kept = append(kept,
		str("shed.project.id", src.ProjectID),
		str("shed.project.name", src.ProjectName),
		str("shed.service.id", src.ServiceID),
		str("shed.service.name", src.ServiceName),
		str("shed.service.kind", src.ServiceKind),
		str("shed.deployment.id", src.DeploymentID),
		str("shed.source", origin),
	)
	if !hasName {
		kept = append(kept, str(serviceNameKey, src.ServiceName))
	}
	res.Attributes = kept
	return res
}

// reserved reports whether a record attribute key lands in shed's
// namespace in a store: VictoriaMetrics turns every character but a
// letter, digit, or underscore into an underscore (shed.service.id becomes
// shed_service_id), and VictoriaLogs flattens a nested shed value into
// shed.* fields.
func reserved(key string) bool {
	rest, ok := strings.CutPrefix(key, "shed")
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	c := rest[0]
	return !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9')
}

// dropReserved removes the attributes whose keys are reserved from kvs.
func dropReserved(kvs []*commonpb.KeyValue) []*commonpb.KeyValue {
	return slices.DeleteFunc(kvs, func(kv *commonpb.KeyValue) bool { return reserved(kv.GetKey()) })
}

func str(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}

// maskAttrs masks every string value in kvs, including those nested in
// arrays and key-value lists. A nil mask leaves them alone.
func maskAttrs(kvs []*commonpb.KeyValue, mask func(string) string) {
	if mask == nil {
		return
	}
	for _, kv := range kvs {
		maskValue(kv.GetValue(), mask)
	}
}

func maskValue(v *commonpb.AnyValue, mask func(string) string) {
	if mask == nil {
		return
	}
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		x.StringValue = mask(x.StringValue)
	case *commonpb.AnyValue_ArrayValue:
		for _, e := range x.ArrayValue.GetValues() {
			maskValue(e, mask)
		}
	case *commonpb.AnyValue_KvlistValue:
		maskAttrs(x.KvlistValue.GetValues(), mask)
	}
}

// hashAttrs writes kvs to h in key order, so that the same attribute set
// hashes the same whatever order the sender used.
func hashAttrs(h *maphash.Hash, kvs []*commonpb.KeyValue) {
	sorted := slices.SortedFunc(slices.Values(kvs), func(a, b *commonpb.KeyValue) int {
		return cmp.Compare(a.GetKey(), b.GetKey())
	})
	hashUint64(h, uint64(len(sorted)))
	for _, kv := range sorted {
		hashString(h, kv.GetKey())
		hashValue(h, kv.GetValue())
	}
}

func hashUint64(h *maphash.Hash, n uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], n)
	h.Write(b[:])
}

func hashString(h *maphash.Hash, s string) {
	hashUint64(h, uint64(len(s)))
	h.WriteString(s)
}

func hashValue(h *maphash.Hash, v *commonpb.AnyValue) {
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		h.WriteByte(1)
		hashString(h, x.StringValue)
	case *commonpb.AnyValue_BoolValue:
		h.WriteByte(2)
		if x.BoolValue {
			h.WriteByte(1)
		} else {
			h.WriteByte(0)
		}
	case *commonpb.AnyValue_IntValue:
		h.WriteByte(3)
		hashUint64(h, uint64(x.IntValue))
	case *commonpb.AnyValue_DoubleValue:
		h.WriteByte(4)
		hashUint64(h, math.Float64bits(x.DoubleValue))
	case *commonpb.AnyValue_BytesValue:
		h.WriteByte(5)
		hashUint64(h, uint64(len(x.BytesValue)))
		h.Write(x.BytesValue)
	case *commonpb.AnyValue_ArrayValue:
		h.WriteByte(6)
		vs := x.ArrayValue.GetValues()
		hashUint64(h, uint64(len(vs)))
		for _, e := range vs {
			hashValue(h, e)
		}
	case *commonpb.AnyValue_KvlistValue:
		h.WriteByte(7)
		hashAttrs(h, x.KvlistValue.GetValues())
	default:
		h.WriteByte(0)
	}
}
