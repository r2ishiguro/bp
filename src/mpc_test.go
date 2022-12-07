package bp

import (
	"crypto/rand"
	"math/big"
	"crypto/sha256"
	"testing"
	"fmt"
)

type PoB struct {
	proof RangeProof
	gamma *big.Int
	ctx MPRangeContext
}

var svals = []int{40, 60}
var dvals = []int{80, 10, 5, 5}

func TestMPC(t *testing.T) {
	EC = NewECPrimeGroupKey(NBITS)

	var srcs []*PoB
	var dsts []*PoB

	var cp RangeProof	// consolidated proof

	//
	// step 0
	//
	for _, v := range svals {
		gamma, err := rand.Int(rand.Reader, EC.N)
		if err != nil {
			t.Fatal(err)
		}
		var pob PoB
		RPProveStep0(&pob.ctx, &pob.proof, big.NewInt(int64(v)), gamma)
		pob.gamma = gamma
		srcs = append(srcs, &pob)
	}
	for _, v := range dvals {
		gamma, err := rand.Int(rand.Reader, EC.N)
		if err != nil {
			t.Fatal(err)
		}
		var pob PoB
		RPProveStep0(&pob.ctx, &pob.proof, big.NewInt(int64(v)), gamma)
		pob.gamma = gamma
		dsts = append(dsts, &pob)
	}

	// calculate y, z
	h := sha256.New()
	buf := make([]byte, 32)
	for _, pob := range srcs {
		cp.Comm = cp.Comm.Add(pob.proof.Comm)
		cp.A = cp.A.Add(pob.proof.A)
		cp.S = cp.S.Add(pob.proof.S)
	}
	for _, pob := range dsts {
		cp.Comm = cp.Comm.Sub(pob.proof.Comm)
		cp.A = cp.A.Add(pob.proof.A)
		cp.S = cp.S.Add(pob.proof.S)
	}
	cp.Comm.X.FillBytes(buf)	// comm
	h.Write(buf)
	h.Write([]byte{byte(EC.V)})	// n
	cp.A.X.FillBytes(buf)	// A.x
	h.Write(buf)
	cp.S.X.FillBytes(buf)	// S.x
	h.Write(buf)
	chal1s256 := h.Sum(nil)
	cy := new(big.Int).SetBytes(chal1s256)
	cp.Cy = cy
	
	h.Reset()
	cp.A.X.FillBytes(buf)	// A.x
	h.Write(buf)
	cp.S.X.FillBytes(buf)	// S.x
	h.Write(buf)
	h.Write(chal1s256)	// y
	chal2s256 := h.Sum(nil)
	cz := new(big.Int).SetBytes(chal2s256)
	cp.Cz = cz

	//
	// step 1
	//
	for _, pob := range append(srcs, dsts...) {
		pob.proof.Cy = cy
		pob.proof.Cz = cz
		RPProveStep1(&pob.ctx, &pob.proof)
	}

	for _, pob := range srcs {
		cp.T1 = cp.T1.Add(pob.proof.T1)
		cp.T2 = cp.T2.Add(pob.proof.T2)
	}
	for _, pob := range dsts {
		cp.T1 = cp.T1.Sub(pob.proof.T1)
		cp.T2 = cp.T2.Sub(pob.proof.T2)
	}

	// calculate x
	h.Reset()
	h.Write(chal2s256)		// z
	cp.T1.X.FillBytes(buf)		// T1.x
	h.Write(buf)
	cp.T2.X.FillBytes(buf)		// T2.x
	h.Write(buf)
	chal3s256 := h.Sum(nil)
	cx := new(big.Int).SetBytes(chal3s256)
	cp.Cx = cx

	//
	// step 2
	//
	for _, pob := range append(srcs, dsts...) {
		pob.proof.Cx = cx
		RPProveStep2(&pob.ctx, &pob.proof, pob.gamma)
	}

	cp.Factor = 0
	cp.Tau = big.NewInt(0)
	cp.Th = big.NewInt(0)
	cp.Mu = big.NewInt(0)
	th := big.NewInt(0)
	n := len(srcs) + len(dsts)
	cp.L = make([]*big.Int, n * EC.V)
	cp.R = make([]*big.Int, n * EC.V)
	b := 0
	for _, pob := range srcs {
		cp.Tau.Mod(cp.Tau.Add(cp.Tau, pob.proof.Tau), EC.N)
		cp.Th.Mod(cp.Th.Add(cp.Th, pob.proof.Th), EC.N)
		cp.Mu.Mod(cp.Mu.Add(cp.Mu, pob.proof.Mu), EC.N)
		for i, l := range pob.proof.L {
			cp.L[i * n + b] = l
		}
		for i, r := range pob.proof.R {
			cp.R[i * n + b] = r
		}
		b += 1
		cp.Factor += 1
		th.Add(th, InnerProduct(pob.proof.L, pob.proof.R))
	}
	for _, pob := range dsts {
		cp.Tau.Mod(cp.Tau.Add(cp.Tau, new(big.Int).Sub(EC.N, pob.proof.Tau)), EC.N)
		cp.Th.Mod(cp.Th.Add(cp.Th, new(big.Int).Sub(EC.N, pob.proof.Th)), EC.N)
		cp.Mu.Mod(cp.Mu.Add(cp.Mu, pob.proof.Mu), EC.N)
		for i, l := range pob.proof.L {
			cp.L[i * n + b] = l
		}
		for i, r := range pob.proof.R {
			cp.R[i * n + b] = r
		}
		b += 1
		cp.Factor -= 1
		th.Add(th, new(big.Int).Sub(EC.N, InnerProduct(pob.proof.L, pob.proof.R)))
	}
	th.Mod(th, EC.N)

	// interleave g and h
	var iG, iH []ECPoint
	for i := 0; i < len(EC.BPG); i++ {
		for j := 0; j < n; j++ {
			iG = append(iG, EC.BPG[i])
			iH = append(iH, EC.BPH[i])
		}
	}
	EC.BPG = iG
	EC.BPH = iH

	RPProveStep3(&cp)

	//
	// check if \Sum <l,r> ?= t^
	//
	if th.Cmp(cp.Th) != 0 {
		fmt.Println("\\Sum <l,r> != t^")
	}

	cp.LR = InnerProduct(cp.L, cp.R)

	//
	// Verify it
	//
	if !RPVerify(cp) {
		t.Fatalf("Verification failed")
	}

	fmt.Printf("MPC size = %d\n", len(cp.IPP.L) * 2 + 4 + 5)
}
