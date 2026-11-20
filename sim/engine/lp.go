package engine

import "math"

// maximize solves a small linear program: the x >= 0 that maximizes
// objective·x under the equalities and the inequalities (a row holds the
// coefficients and, last, the right-hand side: a·x = b, a·x <= b). It walks
// the corners of the feasible region, which is fine for the handful of
// variables a rotation has. ok is false when nothing is feasible.
func maximize(objective []float64, equal, atMost [][]float64) (best []float64, value float64, ok bool) {
	n := len(objective)
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
