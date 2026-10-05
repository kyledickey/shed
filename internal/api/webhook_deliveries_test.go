package api

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestDeliveryRetryAndExpiry(t *testing.T) {
	var c deliveryCache
	now := time.Now()
	key := deliveryKey{service: "app"}
	if duplicate, busy := c.begin(key, now); duplicate || busy {
		t.Fatal("new delivery blocked")
	}
	if duplicate, busy := c.begin(key, now); duplicate || !busy {
		t.Fatal("concurrent delivery admitted")
	}
	c.finish(key, false)
	if duplicate, busy := c.begin(key, now); duplicate || busy {
		t.Fatal("failed delivery cannot retry")
	}
	c.finish(key, true)
	if duplicate, busy := c.begin(key, now); !duplicate || busy {
		t.Fatal("completed delivery admitted")
	}
	if duplicate, busy := c.begin(key, now.Add(25*time.Hour)); duplicate || busy {
		t.Fatal("expired delivery blocked")
	}
}
func TestDeliveryCacheBound(t *testing.T) {
	var c deliveryCache
	now := time.Now()
	for i := range 1100 {
		key := deliveryKey{service: "app"}
		binary.BigEndian.PutUint64(key.body[:], uint64(i))
		if duplicate, busy := c.begin(key, now); duplicate || busy {
			t.Fatal("new delivery blocked")
		}
		c.finish(key, true)
	}
	if len(c.entries) != 1024 {
		t.Fatalf("cache size %d", len(c.entries))
	}
}
