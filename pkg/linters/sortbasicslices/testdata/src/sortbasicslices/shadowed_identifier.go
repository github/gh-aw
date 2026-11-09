package sortbasicslices

type customSort struct{}

func (customSort) Strings([]string)   {}
func (customSort) Ints([]int)         {}
func (customSort) Float64s([]float64) {}

func GoodShadowedSortIdentifier(items []string, ints []int, floats []float64) {
	sort := customSort{}
	sort.Strings(items)
	sort.Ints(ints)
	sort.Float64s(floats)
}
