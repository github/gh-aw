//go:build !integration

package mathutil

import "testing"

func TestMin(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{
			name:     "a less than b",
			a:        5,
			b:        10,
			expected: 5,
		},
		{
			name:     "b less than a",
			a:        10,
			b:        5,
			expected: 5,
		},
		{
			name:     "equal values",
			a:        7,
			b:        7,
			expected: 7,
		},
		{
			name:     "negative values",
			a:        -5,
			b:        -10,
			expected: -10,
		},
		{
			name:     "mixed signs",
			a:        -5,
			b:        10,
			expected: -5,
		},
		{
			name:     "zero values",
			a:        0,
			b:        0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Min(%d, %d) = %d; want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMax(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{
			name:     "a greater than b",
			a:        10,
			b:        5,
			expected: 10,
		},
		{
			name:     "b greater than a",
			a:        5,
			b:        10,
			expected: 10,
		},
		{
			name:     "equal values",
			a:        7,
			b:        7,
			expected: 7,
		},
		{
			name:     "negative values",
			a:        -5,
			b:        -10,
			expected: -5,
		},
		{
			name:     "mixed signs",
			a:        -5,
			b:        10,
			expected: 10,
		},
		{
			name:     "zero values",
			a:        0,
			b:        0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Max(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Max(%d, %d) = %d; want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func BenchmarkMin(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Min(42, 100)
	}
}

func BenchmarkMax(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Max(42, 100)
	}
}

// Additional edge case tests

func TestMin_LargeNumbers(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{
			name:     "very large positive",
			a:        2147483647, // MaxInt32
			b:        2147483646,
			expected: 2147483646,
		},
		{
			name:     "very large negative",
			a:        -2147483648, // MinInt32
			b:        -2147483647,
			expected: -2147483648,
		},
		{
			name:     "max positive vs zero",
			a:        2147483647,
			b:        0,
			expected: 0,
		},
		{
			name:     "min negative vs zero",
			a:        -2147483648,
			b:        0,
			expected: -2147483648,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Min(%d, %d) = %d; want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMax_LargeNumbers(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{
			name:     "very large positive",
			a:        2147483647, // MaxInt32
			b:        2147483646,
			expected: 2147483647,
		},
		{
			name:     "very large negative",
			a:        -2147483648, // MinInt32
			b:        -2147483647,
			expected: -2147483647,
		},
		{
			name:     "max positive vs zero",
			a:        2147483647,
			b:        0,
			expected: 2147483647,
		},
		{
			name:     "min negative vs zero",
			a:        -2147483648,
			b:        0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Max(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Max(%d, %d) = %d; want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMin_Commutative(t *testing.T) {
	// Test that Min(a, b) == Min(b, a)
	testCases := []struct {
		a, b int
	}{
		{5, 10},
		{-5, 10},
		{-10, -5},
		{0, 5},
		{100, 50},
	}

	for _, tc := range testCases {
		result1 := Min(tc.a, tc.b)
		result2 := Min(tc.b, tc.a)

		if result1 != result2 {
			t.Errorf("Min is not commutative: Min(%d, %d) = %d, but Min(%d, %d) = %d",
				tc.a, tc.b, result1, tc.b, tc.a, result2)
		}
	}
}

func TestMax_Commutative(t *testing.T) {
	// Test that Max(a, b) == Max(b, a)
	testCases := []struct {
		a, b int
	}{
		{5, 10},
		{-5, 10},
		{-10, -5},
		{0, 5},
		{100, 50},
	}

	for _, tc := range testCases {
		result1 := Max(tc.a, tc.b)
		result2 := Max(tc.b, tc.a)

		if result1 != result2 {
			t.Errorf("Max is not commutative: Max(%d, %d) = %d, but Max(%d, %d) = %d",
				tc.a, tc.b, result1, tc.b, tc.a, result2)
		}
	}
}

func TestMinMax_Consistency(t *testing.T) {
	// Test that min(a, b) <= max(a, b) for all a, b
	testCases := []struct {
		a, b int
	}{
		{5, 10},
		{10, 5},
		{-5, 10},
		{-10, -5},
		{0, 0},
		{100, -100},
	}

	for _, tc := range testCases {
		minVal := Min(tc.a, tc.b)
		maxVal := Max(tc.a, tc.b)

		if minVal > maxVal {
			t.Errorf("Min(%d, %d) = %d should be <= Max(%d, %d) = %d",
				tc.a, tc.b, minVal, tc.a, tc.b, maxVal)
		}
	}
}

func TestMin_Identity(t *testing.T) {
	// Test that Min(x, x) == x
	values := []int{-100, -1, 0, 1, 100, 2147483647, -2147483648}

	for _, val := range values {
		result := Min(val, val)
		if result != val {
			t.Errorf("Min(%d, %d) = %d; want %d", val, val, result, val)
		}
	}
}

func TestMax_Identity(t *testing.T) {
	// Test that Max(x, x) == x
	values := []int{-100, -1, 0, 1, 100, 2147483647, -2147483648}

	for _, val := range values {
		result := Max(val, val)
		if result != val {
			t.Errorf("Max(%d, %d) = %d; want %d", val, val, result, val)
		}
	}
}

func BenchmarkMin_Negative(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Min(-42, -100)
	}
}

func BenchmarkMax_Negative(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Max(-42, -100)
	}
}

func BenchmarkMin_Large(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Min(2147483647, 2147483646)
	}
}

func BenchmarkMax_Large(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Max(2147483647, 2147483646)
	}
}
