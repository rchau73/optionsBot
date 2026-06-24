package gateway

import "optionsbot/internal/config"

// RetryConfig returns a short-interval config suitable for unit tests.
func RetryConfig() config.RetryConfig {
	return config.RetryConfig{
		InitialMS:  10,
		Multiplier: 2.0,
		MaxMS:      100,
		MaxRetries: 5,
	}
}
