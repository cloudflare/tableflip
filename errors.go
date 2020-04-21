package tableflip

type ErrNotSupported struct{}

func (e ErrNotSupported) Error() string {
	return "tableflip: platform does not support graceful restart"
}
