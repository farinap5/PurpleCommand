package lua

import (
	"math"
	"testing"
	"time"
)

func TestCallbackTimeoutFromSeconds(t *testing.T) {
	timeout, err := callbackTimeoutFromSeconds(1.5)
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 1500*time.Millisecond {
		t.Fatalf("timeout = %s", timeout)
	}

	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := callbackTimeoutFromSeconds(value); err == nil {
			t.Errorf("timeout %v was accepted", value)
		}
	}
}
