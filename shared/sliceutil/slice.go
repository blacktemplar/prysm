package sliceutil

// SubsetUint64 returns true if the first array is
// completely contained in the second array with time
// complexity of approximately o(n).
func SubsetUint64(a []uint64, b []uint64) bool {
	if len(a) > len(b) {
		return false
	}

	set := make(map[uint64]uint64)
	for _, v := range b {
		set[v]++
	}

	for _, v := range a {
		if count, found := set[v]; !found {
			return false
		} else if count < 1 {
			return false
		} else {
			set[v] = count - 1
		}
	}
	return true
}

// IntersectionUint64 of two uint64 slices with time
// complexity of approximately O(n) leveraging a map to
// check for element existence off by a constant factor
// of underlying map efficiency.
func IntersectionUint64(a []uint64, b []uint64) []uint64 {
	set := make([]uint64, 0)
	m := make(map[uint64]bool)

	for i := 0; i < len(a); i++ {
		m[a[i]] = true
	}
	for i := 0; i < len(b); i++ {
		if _, found := m[b[i]]; found {
			set = append(set, b[i])
		}
	}
	return set
}

// UnionUint64 of two uint64 slices with time
// complexity of approximately O(n) leveraging a map to
// check for element existence off by a constant factor
// of underlying map efficiency.
func UnionUint64(a []uint64, b []uint64) []uint64 {
	set := make([]uint64, 0)
	m := make(map[uint64]bool)

	for i := 0; i < len(a); i++ {
		m[a[i]] = true
		set = append(set, a[i])
	}
	for i := 0; i < len(b); i++ {
		if _, found := m[b[i]]; !found {
			set = append(set, b[i])
		}
	}
	return set
}

// IsUint64Sorted verifies if a uint64 slice is sorted in ascending order.
func IsUint64Sorted(a []uint64) bool {
	if len(a) == 0 || len(a) == 1 {
		return true
	}
	for i := 1; i < len(a); i++ {
		if a[i-1] > a[i] {
			return false
		}
	}
	return true
}

// NotUint64 returns the uint64 in slice a that are
// not in slice b with time complexity of approximately
// O(n) leveraging a map to check for element existence
// off by a constant factor of underlying map efficiency.
func NotUint64(a []uint64, b []uint64) []uint64 {
	set := make([]uint64, 0)
	m := make(map[uint64]bool)

	for i := 0; i < len(a); i++ {
		m[a[i]] = true
	}
	for i := 0; i < len(b); i++ {
		if _, found := m[b[i]]; !found {
			set = append(set, b[i])
		}
	}
	return set
}

// IsInUint64 returns true if a is in b and False otherwise.
func IsInUint64(a uint64, b []uint64) bool {
	for _, v := range b {
		if a == v {
			return true
		}
	}
	return false
}

// IntersectionInt64 of two int64 slices with time
// complexity of approximately O(n) leveraging a map to
// check for element existence off by a constant factor
// of underlying map efficiency.
func IntersectionInt64(a []int64, b []int64) []int64 {
	set := make([]int64, 0)
	m := make(map[int64]bool)

	for i := 0; i < len(a); i++ {
		m[a[i]] = true
	}
	for i := 0; i < len(b); i++ {
		if _, found := m[b[i]]; found {
			set = append(set, b[i])
		}
	}
	return set
}

// UnionInt64 of two int64 slices with time
// complexity of approximately O(n) leveraging a map to
// check for element existence off by a constant factor
// of underlying map efficiency.
func UnionInt64(a []int64, b []int64) []int64 {
	set := make([]int64, 0)
	m := make(map[int64]bool)

	for i := 0; i < len(a); i++ {
		m[a[i]] = true
		set = append(set, a[i])
	}
	for i := 0; i < len(b); i++ {
		if _, found := m[b[i]]; !found {
			set = append(set, b[i])
		}
	}
	return set
}

// NotInt64 returns the int64 in slice a that are
// not in slice b with time complexity of approximately
// O(n) leveraging a map to check for element existence
// off by a constant factor of underlying map efficiency.
func NotInt64(a []int64, b []int64) []int64 {
	set := make([]int64, 0)
	m := make(map[int64]bool)

	for i := 0; i < len(a); i++ {
		m[a[i]] = true
	}
	for i := 0; i < len(b); i++ {
		if _, found := m[b[i]]; !found {
			set = append(set, b[i])
		}
	}
	return set
}

// IsInInt64 returns true if a is in b and False otherwise.
func IsInInt64(a int64, b []int64) bool {
	for _, v := range b {
		if a == v {
			return true
		}
	}
	return false
}

// ByteIntersection returns a new set with elements that are common in
// both sets a and b.
func ByteIntersection(a []byte, b []byte) []byte {
	set := make([]byte, 0)
	m := make(map[byte]bool)

	for i := 0; i < len(a); i++ {
		m[a[i]] = true
	}
	for i := 0; i < len(b); i++ {
		if _, found := m[b[i]]; found {
			set = append(set, b[i])
		}
	}
	return set
}

// ByteUnion returns a new set with elements that are common in
// both sets a and b.
func ByteUnion(a []byte, b []byte) []byte {
	set := make([]byte, 0)
	m := make(map[byte]bool)

	for i := 0; i < len(a); i++ {
		m[a[i]] = true
		set = append(set, a[i])
	}
	for i := 0; i < len(b); i++ {
		if _, found := m[b[i]]; !found {
			set = append(set, b[i])
		}
	}
	return set
}

// ByteNot returns a new set with elements that are common in
// both sets a and b.
func ByteNot(a []byte, b []byte) []byte {
	set := make([]byte, 0)
	m := make(map[byte]bool)

	for i := 0; i < len(a); i++ {
		m[a[i]] = true
	}
	for i := 0; i < len(b); i++ {
		if _, found := m[b[i]]; !found {
			set = append(set, b[i])
		}
	}
	return set
}

// ByteIsIn returns true if a is in b and False otherwise.
func ByteIsIn(a byte, b []byte) bool {
	for _, v := range b {
		if a == v {
			return true
		}
	}
	return false
}
