package customerror

type error string

type Closer struct{}

func (Closer) Close() error {
	return ""
}
