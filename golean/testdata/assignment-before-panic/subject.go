package main

type S0 struct {
	f0 int
	f1 uint8
	f2 string
}

func h0(p0 int) uint64 {
	for i0 := 0; i0 < 5; i0++ {
		p0 += i0
		break
	}
	p0 = (int(p0) << 7)
	p0 = -2147483647
	return ((uint64(p0) % uint64(18446744073709551614)) >> 0)
}

func h1(p0 int, p1 uint8) (string, int16) {
	_, _ = p0, ((-p0) << 32)
	p0 = ((p0 % -1) % 7)
	p0 = (((p0 << 4) + (int(p0) >> 4)) - (int(p0) >> 6))
	return "", int16(21845)
}

type T0 uint

func fuzzSubject() (q0 int, q1 int8, q2 int16, q3 int32, q4 int64, q5 uint, q6 uint8, q7 uint16, q8 uint32, q9 uint64, q10 bool, q11 string, q12 [2]int16, q13 [4]uint64, q14 T0, q15 S0, q16 string, q17 string, q18 int) {
	v0 := 2147483646
	v1 := int8(85)
	v2 := int16(-34)
	v3 := int32(7)
	v4 := int64(-1)
	v5 := uint(4294967295)
	v6 := uint8(11)
	v7 := uint16(19)
	v8 := uint32(2863311530)
	v9 := uint64(7)
	v10 := true
	v11 := "gros"
	v12 := [2]int16{int16(32766), int16(-1)}
	v13 := [4]uint64{uint64(47), uint64(18446744073709551615), uint64(12297829382473034410), uint64(18)}
	v14 := []uint64{uint64(19), uint64(9223372036854775808), uint64(23)}
	v15 := []int64{int64(9223372036854775807), int64(30), int64(9223372036854775807), int64(-36)}
	v16 := map[uint8]uint{uint8(6): uint(4294967295), uint8(7): uint(26)}
	v17 := T0(2863311530)
	v18 := S0{f0: -8, f1: uint8(35), f2: "fuzz"}
	v19 := map[uint8]uint{uint8(1): uint(20), uint8(0): uint(2863311530)}
	v20 := "go"
	v21 := []int64{int64(-9), int64(9223372036854775807)}
	v22 := "x"
	psite := 0
	defer func() {
		if recover() != nil {
			q18 = psite
			q0 = v0
			q1 = v1
			q2 = v2
			q3 = v3
			q4 = v4
			q5 = v5
			q6 = v6
			q7 = v7
			q8 = v8
			q9 = v9
			q10 = v10
			q11 = v11
			q12 = v12
			q13 = v13
			q14 = v17
			q15 = v18
			q16 = v20
			q17 = v22
		}
	}()
	psite = 1
	if v10 {
		v16[uint8(6)] = ((v16[uint8(2)] >> 3) - (-(v5 >> 31)))
	} else {
		v14 = append(v14, v13[v0])
		v16[uint8(7)] = v5
	}
	psite = 2
	v18.f0 = v18.f0
	psite = 3
	v11, v12[v0] = h1(len(v16), v18.f1)
	psite = 4
	v20 = "gros"
	psite = 5
	v18.f0 = v0
	psite = 6
	if !((v3 >> 31) != v3) {
		v18.f1 = v6
		for i1 := 0; i1 < 5; i1++ {
			v0 += i1
			v7, _ = ((-v7) << 7), (-(v0 % 14))
			v14 = append(v14, v13[v0])
		}
	} else {
		v1 = (-(-(v1 % v1)))
	}
	psite = 7
	v6 = (v6 << 7)
	psite = 8
	v5++
	psite = 9
	v15 = append(v15, v4)
	psite = 10
	for i2 := 0; i2 < 4; i2++ {
		v0 += i2
		v21 = append(v21, v4)
	}
	psite = 11
	v16[uint8(7)] = (v5 % uint(8))
	psite = 12
	v14 = append(v14, (v9 >> 65))
	psite = 13
	v15 = append(v15, v4)
	psite = 14
	v18.f0 = (v0 + ((v0 << 31) * (v0 + -2147483647)))
	psite = 15
	_ = v19
	return v0, v1, v2, v3, v4, v5, v6, v7, v8, v9, v10, v11, v12, v13, v17, v18, v20, v22, 0
}
