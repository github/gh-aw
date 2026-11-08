package other

type Cleaner struct{}

func (Cleaner) Remove(string) error {
	return nil
}

func (Cleaner) RemoveAll(string) error {
	return nil
}
