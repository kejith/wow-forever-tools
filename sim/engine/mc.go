package engine

import (
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"sync"
)

// The Monte-Carlo stage: the same priority rotation, played cast by cast
// over many simulated fights with real rolls (hit, crit, damage range).
// The player itself is in play.go.
//
// Evaluations share the per-iteration seed (common random numbers), so the
// difference between two gear states comes from the gear, not the dice.

// MCStats summarizes the sampled fights.
type MCStats struct {
	Iterations int     `json:"iterations"`
	Mean       float64 `json:"mean"`
	StdDev     float64 `json:"stdDev"`
	P10        float64 `json:"p10"`
	P90        float64 `json:"p90"`
}

// RunMC plays the rotation `iterations` times and reports the metric's
// distribution. The metric follows Evaluate: dps, hps or ttoom.
func RunMC(st *State, fight Fight, actions []Action, metric string, iterations int, seed int64) MCStats {
	if iterations <= 0 {
		iterations = 10000
	}
	p := newPlayer(st, fight, actions, metric)
	values := make([]float64, iterations)
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if iterations < 256 || workers == 1 {
		runMCChunk(p, values, 0, iterations, metric, uint64(seed))
	} else {
		if workers > iterations {
			workers = iterations
		}
		var wg sync.WaitGroup
		wg.Add(workers)
		chunk := (iterations + workers - 1) / workers
		for w := 0; w < workers; w++ {
			lo := w * chunk
			hi := lo + chunk
			if hi > iterations {
				hi = iterations
			}
			if lo >= hi {
				wg.Done()
				continue
			}
			go func(lo, hi int) {
				defer wg.Done()
				runMCChunk(p, values, lo, hi, metric, uint64(seed))
			}(lo, hi)
		}
		wg.Wait()
	}
	return summarize(values)
}

func runMCChunk(p *player, values []float64, lo, hi int, metric string, seed uint64) {
	sc := p.newScratch()
	pcg := rand.NewPCG(seed, 0)
	rng := rand.New(pcg)
	for i := lo; i < hi; i++ {
		pcg.Seed(seed, uint64(i)+1)
		value, oom := p.playWith(rng, nil, &sc)
		if metric == "ttoom" {
			value = oom
		}
		values[i] = value
	}
}

func summarize(values []float64) MCStats {
	n := len(values)
	s := MCStats{Iterations: n}
	if n == 0 {
		return s
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	s.Mean = sum / float64(n)
	var sq float64
	for _, v := range values {
		d := v - s.Mean
		sq += d * d
	}
	s.StdDev = math.Sqrt(sq / float64(n))
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	s.P10 = sorted[n/10]
	s.P90 = sorted[(n*9)/10]
	return s
}
