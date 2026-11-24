package engine

import "math"

// maximize solves a small linear program: the x >= 0 that maximizes
// objective·x under the equalities and the inequalities (a row holds the
// coefficients and, last, the right-hand side: a·x = b, a·x <= b). It walks
// the corners of the feasible region, which is fine for the handful of
// variables a rotation has. ok is false when nothing is feasible.
func maximize(objective []float64, equal, atMost [][]float64) (best []float64, value float64, ok bool) {
	n := len(objective)
	if n == 4 && len(equal) <= 4 && len(atMost) <= 8 {
		return maximize4(objective, equal, atMost)
	}
	// Every bound a corner can sit on: the inequalities, then x[i] >= 0.
	bounds := append([][]float64(nil), atMost...)
	for i := 0; i < n; i++ {
		row := make([]float64, n+1)
		row[i] = 1
		bounds = append(bounds, row)
	}
	pick := n - len(equal) // bounds that are tight in a corner
	if pick < 0 || pick > len(bounds) {
		return nil, 0, false
	}
	const eps = 1e-9
	chosen := make([]int, pick)
	var walk func(from, depth int)
	walk = func(from, depth int) {
		if depth < pick {
			for i := from; i < len(bounds); i++ {
				chosen[depth] = i
				walk(i+1, depth+1)
			}
			return
		}
		system := make([][]float64, 0, n)
		for _, row := range equal {
			system = append(system, append([]float64(nil), row...))
		}
		for _, i := range chosen {
			system = append(system, append([]float64(nil), bounds[i]...))
		}
		x, solved := solveLinear(system)
		if !solved {
			return
		}
		for i := range x {
			if x[i] < -eps {
				return
			}
			x[i] = math.Max(0, x[i])
		}
		for _, row := range atMost {
			sum := 0.0
			for i := range x {
				sum += row[i] * x[i]
			}
			if sum > row[n]+eps*(1+math.Abs(row[n])) {
				return
			}
		}
		v := 0.0
		for i := range x {
			v += objective[i] * x[i]
		}
		if !ok || v > value {
			best, value, ok = x, v, true
		}
	}
	walk(0, 0)
	return best, value, ok
}

// maximize4 is the zero-allocation fast path for 4-variable LPs (used by Evaluate).
func maximize4(objective []float64, equal, atMost [][]float64) ([]float64, float64, bool) {
	var bounds [12][5]float64
	nb := 0
	for _, row := range atMost {
		copy(bounds[nb][:], row)
		nb++
	}
	for i := 0; i < 4; i++ {
		bounds[nb][i] = 1
		nb++
	}
	ne := len(equal)
	pick := 4 - ne
	if pick < 0 || pick > nb {
		return nil, 0, false
	}
	const eps = 1e-9
	var chosen [4]int
	var bestArr [4]float64
	var value float64
	ok := false
	var walk func(from, depth int)
	walk = func(from, depth int) {
		if depth < pick {
			for i := from; i < nb; i++ {
				chosen[depth] = i
				walk(i+1, depth+1)
			}
			return
		}
		var sys [4][5]float64
		for i := 0; i < ne; i++ {
			copy(sys[i][:], equal[i])
		}
		for i := 0; i < pick; i++ {
			sys[ne+i] = bounds[chosen[i]]
		}
		x, solved := solveLinear4(&sys)
		if !solved {
			return
		}
		for i := 0; i < 4; i++ {
			if x[i] < -eps {
				return
			}
			if x[i] < 0 {
				x[i] = 0
			}
		}
		for _, row := range atMost {
			sum := row[0]*x[0] + row[1]*x[1] + row[2]*x[2] + row[3]*x[3]
			if sum > row[4]+eps*(1+math.Abs(row[4])) {
				return
			}
		}
		v := objective[0]*x[0] + objective[1]*x[1] + objective[2]*x[2] + objective[3]*x[3]
		if !ok || v > value {
			bestArr, value, ok = x, v, true
		}
	}
	walk(0, 0)
	if !ok {
		return nil, 0, false
	}
	return []float64{bestArr[0], bestArr[1], bestArr[2], bestArr[3]}, value, true
}

func solveLinear4(rows *[4][5]float64) ([4]float64, bool) {
	var x [4]float64
	for col := 0; col < 4; col++ {
		pivot := col
		for r := col + 1; r < 4; r++ {
			if math.Abs(rows[r][col]) > math.Abs(rows[pivot][col]) {
				pivot = r
			}
		}
		if math.Abs(rows[pivot][col]) < 1e-12 {
			return x, false
		}
		if pivot != col {
			rows[col], rows[pivot] = rows[pivot], rows[col]
		}
		piv := rows[col][col]
		for r := 0; r < 4; r++ {
			if r == col {
				continue
			}
			k := rows[r][col] / piv
			if k == 0 {
				continue
			}
			for c := col; c <= 4; c++ {
				rows[r][c] -= k * rows[col][c]
			}
		}
	}
	for i := 0; i < 4; i++ {
		x[i] = rows[i][4] / rows[i][i]
	}
	return x, true
}

// solveLinear solves a square system by elimination; a row holds the
// coefficients and the right-hand side. The rows are changed.
func solveLinear(rows [][]float64) ([]float64, bool) {
	n := len(rows)
	for col := 0; col < n; col++ {
		pivot := col
		for r := col + 1; r < n; r++ {
			if math.Abs(rows[r][col]) > math.Abs(rows[pivot][col]) {
				pivot = r
			}
		}
		if math.Abs(rows[pivot][col]) < 1e-12 {
			return nil, false
		}
		rows[col], rows[pivot] = rows[pivot], rows[col]
		for r := 0; r < n; r++ {
			if r == col {
				continue
			}
			k := rows[r][col] / rows[col][col]
			if k == 0 {
				continue
			}
			for c := col; c <= n; c++ {
				rows[r][c] -= k * rows[col][c]
			}
		}
	}
	x := make([]float64, n)
	for i := range x {
		x[i] = rows[i][n] / rows[i][i]
	}
	return x, true
}
