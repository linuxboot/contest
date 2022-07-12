package prometheus

// Option is an optional argument to function New, that changes the behavior of the metrics handler.
type Option interface {
	apply(*config)
}

type options []Option

func (s options) Config() config {
	var cfg config
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type config struct {
	DisableLabels bool
}

// OptionDisableLabels disables the labels (tags) and makes all the metrics flat.
// Attempts to get a metrics with the same key but different tags will result into
// getting the same metric. And all the metrics will be registered without labels.
type OptionDisableLabels bool

func (opt OptionDisableLabels) apply(cfg *config) {
	cfg.DisableLabels = bool(opt)
}
