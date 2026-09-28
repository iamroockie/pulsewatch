package domain

import "errors"

var (
	ErrMonitorNotFound        = errors.New("monitor not found")
	ErrClaimLost              = errors.New("claim on monitor is no longer held")
	ErrInvalidURL             = errors.New("invalid url")
	ErrUnsupportedSchemeURL   = errors.New("unsupported url scheme")
	ErrCredentialsInURL       = errors.New("url contains credentials")
	ErrIntervalOutOfRange     = errors.New("interval out of range")
	ErrIntervalPrecision      = errors.New("interval is not a whole number of seconds")
	ErrTimeoutOutOfRange      = errors.New("timeout out of range")
	ErrTimeoutPrecision       = errors.New("timeout is not a whole number of milliseconds")
	ErrTimeoutExceedsInterval = errors.New("timeout exceeds interval")
	ErrMaxRetriesOutOfRange   = errors.New("max retries out of range")
)
