package clustering

import "math"

// adjustedMutualInfo computes the Adjusted Mutual Information of two label
// vectors (Vinh et al. 2010): AMI = (MI − E[MI]) / (max(H_a, H_b) − E[MI]). It
// subtracts the mutual information expected by chance under a hypergeometric
// null, so — unlike raw NMI — it does not inflate as the number of clusters
// grows. ~1 = identical partitions, ~0 = no more agreement than random.
func adjustedMutualInfo(a, b []int) float64 {
	n := len(a)
	if n == 0 || len(b) != n {
		return 0
	}
	countA := map[int]int{}
	countB := map[int]int{}
	countAB := map[[2]int]int{}
	for i := 0; i < n; i++ {
		countA[a[i]]++
		countB[b[i]]++
		countAB[[2]int{a[i], b[i]}]++
	}
	N := float64(n)

	mi := 0.0
	for pair, nab := range countAB {
		pab := float64(nab) / N
		pa := float64(countA[pair[0]]) / N
		pb := float64(countB[pair[1]]) / N
		mi += pab * math.Log(pab/(pa*pb))
	}

	ha := entropyInt(countA, N)
	hb := entropyInt(countB, N)
	emi := expectedMutualInfo(countA, countB, n)

	denom := math.Max(ha, hb) - emi
	if denom <= 1e-12 {
		if ha == 0 && hb == 0 {
			return 1 // both partitions trivial and identical
		}
		return 0
	}
	ami := (mi - emi) / denom
	if ami < 0 {
		ami = 0
	}
	if ami > 1 {
		ami = 1
	}
	return ami
}

// expectedMutualInfo is E[MI] under the hypergeometric (permutation) null model
// given the two partitions' cluster sizes — the exact Vinh et al. expression,
// evaluated in log-space via lgamma to avoid factorial overflow.
func expectedMutualInfo(countA, countB map[int]int, n int) float64 {
	N := float64(n)
	logN := lgammaFact(n)
	emi := 0.0
	for _, ai := range countA {
		for _, bj := range countB {
			lo := ai + bj - n
			if lo < 1 {
				lo = 1
			}
			hi := ai
			if bj < hi {
				hi = bj
			}
			for nij := lo; nij <= hi; nij++ {
				fnij := float64(nij)
				term := (fnij / N) * math.Log(N*fnij/float64(ai*bj))
				logW := lgammaFact(ai) + lgammaFact(bj) + lgammaFact(n-ai) + lgammaFact(n-bj) -
					logN - lgammaFact(nij) - lgammaFact(ai-nij) - lgammaFact(bj-nij) - lgammaFact(n-ai-bj+nij)
				emi += term * math.Exp(logW)
			}
		}
	}
	return emi
}

// normalizedMutualInfo computes NMI of two label vectors (same index = same
// node), in [0,1]. 1 = identical partitions; ~0 = independent. Both empty or
// single-cluster on both sides counts as full agreement.
func normalizedMutualInfo(a, b []int) float64 {
	n := len(a)
	if n == 0 || len(b) != n {
		return 0
	}
	countA := map[int]float64{}
	countB := map[int]float64{}
	countAB := map[[2]int]float64{}
	for i := 0; i < n; i++ {
		countA[a[i]]++
		countB[b[i]]++
		countAB[[2]int{a[i], b[i]}]++
	}
	N := float64(n)

	mi := 0.0
	for pair, nab := range countAB {
		pab := nab / N
		pa := countA[pair[0]] / N
		pb := countB[pair[1]] / N
		mi += pab * math.Log(pab/(pa*pb))
	}

	ha := entropy(countA, N)
	hb := entropy(countB, N)
	if ha == 0 && hb == 0 {
		return 1 // both are a single cluster — trivially identical
	}
	if ha == 0 || hb == 0 {
		return 0 // one side is a single cluster, the other is not
	}
	nmi := mi / math.Sqrt(ha*hb)
	if nmi < 0 {
		nmi = 0
	}
	if nmi > 1 {
		nmi = 1
	}
	return nmi
}

// entropyInt is the Shannon entropy of integer cluster counts.
func entropyInt(counts map[int]int, N float64) float64 {
	h := 0.0
	for _, c := range counts {
		p := float64(c) / N
		if p > 0 {
			h -= p * math.Log(p)
		}
	}
	return h
}

func entropy(counts map[int]float64, N float64) float64 {
	h := 0.0
	for _, c := range counts {
		p := c / N
		if p > 0 {
			h -= p * math.Log(p)
		}
	}
	return h
}

// lgammaFact returns log(k!) = lgamma(k+1).
func lgammaFact(k int) float64 {
	v, _ := math.Lgamma(float64(k) + 1)
	return v
}

func roundTo(v float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(v*pow) / pow
}
