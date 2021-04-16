package lsh

import (
	"bytes"
	"encoding/gob"
	"errors"
	"gonum.org/v1/gonum/blas/blas64"
	"math"
	"math/rand"
	"strconv"
	"sync"
	"time"

	cm "github.com/gasparian/similarity-search-go/lsh/common"
)

// Plane struct holds data needed to work with plane
type Plane struct {
	Coefs blas64.Vector
	D     float64
}

// HasherInstance holds data for local sensetive hashing algorithm
type HasherInstance struct {
	Planes []Plane
}

// Config holds all needed constants for creating the Hasher instance
type Config struct {
	IsAngularDistance int
	NPermutes         int
	NPlanes           int
	BiasMultiplier    float64
	DistanceThrsh     float64
	Dims              int
	Bias              float64
	MeanVec           blas64.Vector
}

// Hasher holds N_PERMUTS number of HasherInstance instances
type Hasher struct {
	mutex           sync.RWMutex
	Config          Config
	Instances       []HasherInstance
	HashFieldsNames []string
}

// SafeHashesHolder allows to lock map while write values in it
type safeHashesHolder struct {
	sync.Mutex
	v map[int]uint64
}

// GetHash calculates LSH code
func (lshInstance *HasherInstance) GetHash(inpVec, meanVec blas64.Vector) uint64 {
	var hash uint64
	shiftedVec := cm.NewVec(make([]float64, inpVec.N))
	blas64.Copy(inpVec, shiftedVec)
	blas64.Axpy(-1.0, meanVec, shiftedVec)
	vec := cm.NewVec(make([]float64, inpVec.N))
	var dp float64
	var dpSign bool
	for i, plane := range lshInstance.Planes {
		blas64.Copy(shiftedVec, vec)
		dp = blas64.Dot(vec, plane.Coefs) - plane.D
		dpSign = math.Signbit(dp)
		if !dpSign {
			hash |= (1 << i)
		}
	}
	return hash
}

// NewLSHIndex creates slice of LSHIndexInstances to hold several permutations results
func NewLSHIndex(config Config) *Hasher {
	lshIndex := &Hasher{
		Config:          config,
		Instances:       make([]HasherInstance, config.NPermutes),
		HashFieldsNames: make([]string, config.NPermutes),
	}
	return lshIndex
}

// GetRandomPlane generates random coefficients of a plane
func (lshIndex *Hasher) getRandomPlane() blas64.Vector {
	coefs := make([]float64, lshIndex.Config.Dims+1)
	var l2 float64 = 0.0
	for i := 0; i < lshIndex.Config.Dims; i++ {
		coefs[i] = -1.0 + rand.Float64()*2
		l2 += coefs[i] * coefs[i]
	}
	l2 = math.Sqrt(l2)
	bias := l2 * lshIndex.Config.Bias
	coefs[len(coefs)-1] = -1.0*bias + rand.Float64()*bias*2
	return cm.NewVec(coefs)
}

// newHasherInstance creates set of planes which will be used to calculate hash
func (lshIndex *Hasher) newHasherInstance() (HasherInstance, error) {
	if lshIndex.Config.Dims <= 0 {
		return HasherInstance{}, errors.New("dimensions number must be a positive integer")
	}
	rand.Seed(time.Now().UnixNano())
	lshInstance := HasherInstance{}
	var coefs blas64.Vector
	for i := 0; i < lshIndex.Config.NPlanes; i++ {
		coefs = lshIndex.getRandomPlane()
		lshInstance.Planes = append(lshInstance.Planes, Plane{
			Coefs: cm.NewVec(coefs.Data[:coefs.N-1]),
			D:     coefs.Data[coefs.N-1],
		})
	}
	return lshInstance, nil
}

// Generate method creates the lsh instances
func (lshIndex *Hasher) Generate(convMean, convStd blas64.Vector) error {
	lshIndex.mutex.Lock()
	defer lshIndex.mutex.Unlock()

	if lshIndex.Config.IsAngularDistance == 1 {
		blas64.Scal(0.0, convStd)
	}
	lshIndex.Config.MeanVec = convMean
	lshIndex.Config.Bias = blas64.Nrm2(convStd) * lshIndex.Config.BiasMultiplier

	var tmpLSHIndex HasherInstance
	var err error
	for i := 0; i < lshIndex.Config.NPermutes; i++ {
		tmpLSHIndex, err = lshIndex.newHasherInstance()
		if err != nil {
			return err
		}
		lshIndex.Instances[i] = tmpLSHIndex
		lshIndex.HashFieldsNames[i] = strconv.Itoa(i)
	}
	return nil
}

// GetHashes returns map of calculated lsh values
func (lshIndex *Hasher) GetHashes(vec blas64.Vector) map[int]uint64 {
	lshIndex.mutex.RLock()
	defer lshIndex.mutex.RUnlock()

	hashes := &safeHashesHolder{v: make(map[int]uint64)}
	wg := sync.WaitGroup{}
	wg.Add(len(lshIndex.Instances))
	for i, lshInstance := range lshIndex.Instances {
		go func(i int, lsh HasherInstance, hashes *safeHashesHolder) {
			hashes.Lock()
			hashes.v[i] = lsh.GetHash(vec, lshIndex.Config.MeanVec)
			hashes.Unlock()
			wg.Done()
		}(i, lshInstance, hashes)
	}
	wg.Wait()
	return hashes.v
}

// GetDist returns measure of the specified distance metric
func (lshIndex *Hasher) GetDist(lv, rv blas64.Vector) (float64, bool) {
	lshIndex.mutex.Lock()
	defer lshIndex.mutex.Unlock()
	var dist float64 = 0.0
	if lshIndex.Config.IsAngularDistance == 1 {
		if cm.IsZeroVector(lv) || cm.IsZeroVector(rv) {
			return 1.0, false // NOTE: zero vectors are wrong with angular metric
		}
		dist = cm.CosineSim(lv, rv)
	} else {
		dist = cm.L2(lv, rv)
	}
	if dist <= lshIndex.Config.DistanceThrsh {
		return dist, true
	}
	return dist, false
}

// Dump encodes Hasher object as a byte-array
func (lshIndex *Hasher) Dump() ([]byte, error) {
	lshIndex.mutex.RLock()
	defer lshIndex.mutex.RUnlock()

	if len(lshIndex.Instances) == 0 {
		return nil, errors.New("search index must contain at least one object")
	}
	buf := &bytes.Buffer{}
	enc := gob.NewEncoder(buf)
	err := enc.Encode(lshIndex)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Load loads Hasher struct from the byte-array file
func (lshIndex *Hasher) Load(inp []byte) error {
	lshIndex.mutex.Lock()
	defer lshIndex.mutex.Unlock()

	buf := &bytes.Buffer{}
	buf.Write(inp)
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&lshIndex)
	if err != nil {
		return err
	}
	return nil
}