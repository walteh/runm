package ticker_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/walteh/runm/pkg/ticker"
)

func TestTicker_New(t *testing.T) {
	ticker := ticker.NewTicker()

	assert.Equal(t, 1*time.Second, ticker.Opts().Interval())
	assert.Equal(t, 5, ticker.Opts().StartBurst())
	assert.Equal(t, 60, ticker.Opts().Frequency())
	assert.Equal(t, "ticker running", ticker.Opts().Message())
}

func TestTicker_WithOptions(t *testing.T) {
	ticker := ticker.NewTicker(
		ticker.WithInterval(2*time.Second),
		ticker.WithStartBurst(10),
		ticker.WithFrequency(30),
		ticker.WithMessage("custom message"),
	)

	assert.Equal(t, 2*time.Second, ticker.Opts().Interval())
	assert.Equal(t, 10, ticker.Opts().StartBurst())
	assert.Equal(t, 30, ticker.Opts().Frequency())
	assert.Equal(t, "custom message", ticker.Opts().Message())
}
