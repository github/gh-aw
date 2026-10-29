package sortbasic

type customSort struct{}

func (customSort) Strings(_ []string) {}

func GoodShadowedSortIdentifier(items []string) {
	sort := customSort{}
	sort.Strings(items)
}
