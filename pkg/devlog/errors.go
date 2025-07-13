package devlog

import (
	"gitlab.com/tozd/go/errors"
)

var (
	// ErrConsumerClosed is returned when operations are attempted on a closed consumer
	ErrConsumerClosed = errors.New("consumer is closed")

	// ErrProducerClosed is returned when operations are attempted on a closed producer
	ErrProducerClosed = errors.New("producer is closed")

	// ErrInvalidEntryType is returned when an entry has an invalid type
	ErrInvalidEntryType = errors.New("invalid entry type")

	// ErrMissingData is returned when required data is missing from an entry
	ErrMissingData = errors.New("missing required data")

	// ErrUnsupportedSourceType is returned when a producer receives unsupported data
	ErrUnsupportedSourceType = errors.New("unsupported source data type")
)
