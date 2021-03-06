package annbench_test

import (
	bench "github.com/gasparian/lsh-search-go/annbench"
	lsh "github.com/gasparian/lsh-search-go/lsh"
	"github.com/gasparian/lsh-search-go/store/kv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	tol = 1e-6
)

func testIndexer(t *testing.T, indexer lsh.Indexer, data *bench.BenchData, config *bench.SearchConfig) {
	start := time.Now()
	t.Logf("Creating search index (%v vectors) ...", len(data.TrainVecs))
	indexer.Train(data.TrainVecs, data.TrainIds)
	t.Logf("Training finished in %v", time.Since(start))

	t.Log("Predicting...")
	start = time.Now()
	N := 10000 // NOTE: for debug it's convenient to change this to lower value in sake of speed up (default is 10k)
	batchSize := 1000
	var elapsedTimeMs int64
	predCh := make(chan bench.Prediction, N)
	wg := sync.WaitGroup{}
	for i := 0; i < len(data.Test[:N]); i += batchSize {
		wg.Add(1)
		end := i + batchSize
		if end > len(data.Test[:N]) {
			end = len(data.Test[:N])
		}
		go func(batch [][]float64, startIdx int, wg *sync.WaitGroup) {
			defer wg.Done()
			for j := range batch {
				start := time.Now()
				closest, err := indexer.Search(batch[j], config.MaxNN, config.MaxDist)
				if err != nil {
					panic(err)
				}
				atomic.AddInt64(&elapsedTimeMs, int64(time.Since(start)/time.Millisecond))
				predCh <- bench.Prediction{Neighbors: closest, Idx: startIdx + j}
			}
		}(data.Test[i:end], i, &wg)
	}
	wg.Wait()
	close(predCh)

	precision, recall := 0.0, 0.0
	for pred := range predCh {
		closestPointsIds := make([]int, 0)
		for _, closest := range pred.Neighbors {
			closestPointsIds = append(closestPointsIds, data.TrainIndices[closest.ID])
			// if config.Metric.IsAngular() {
			// 	pred.Neighbors[i].Dist = lsh.AngularToCosineDist(closest.Dist)
			// }
		}
		p, r := bench.DistanceBasedPrecisionRecall(
			closestPointsIds,
			data.Neighbors[pred.Idx][:config.MaxNN],
			pred.Neighbors,
			data.Distances[pred.Idx][:config.MaxNN],
			config.Epsilon,
		)
		precision += p
		recall += r
	}
	overallElapsedTime := time.Since(start)

	testDataLen := float64(len(data.Test[:N]))

	precision /= testDataLen
	recall /= testDataLen
	avgPredTime := float64(elapsedTimeMs) / testDataLen

	t.Log("Done! Precision: ", precision, "Recall: ", recall)
	t.Logf("Concurrent prediction finished in %v", overallElapsedTime)
	t.Logf("Average prediction time is %v ms", avgPredTime)
}

func testNearestNeighbors(t *testing.T, config *bench.SearchConfig, data *bench.BenchData) {
	s := kv.NewKVStore()
	nn := bench.NewNNMock(config.MaxCandidates, s, config.Metric)
	testIndexer(t, nn, data, config)
}

func testLSH(t *testing.T, config *bench.SearchConfig, data *bench.BenchData) {
	lshConfig := lsh.Config{
		IndexConfig: lsh.IndexConfig{
			BatchSize:     config.BatchSize,
			MaxCandidates: config.MaxCandidates,
		},
		HasherConfig: lsh.HasherConfig{
			NTrees:   config.NTrees,
			KMinVecs: config.KMinVecs,
			Dims:     config.NDims,
		},
	}
	s := kv.NewKVStore()
	lshIndex, err := lsh.NewLsh(lshConfig, s, config.Metric)
	if err != nil {
		t.Fatal(err)
	}
	testIndexer(t, lshIndex, data, config)
}

func TestEuclideanFashionMnist(t *testing.T) {
	dataConfig := &bench.BenchDataConfig{
		DatasetPath:  "../test-data/fashion-mnist-784-euclidean.hdf5",
		SampleSize:   30000,
		TrainDim:     784,
		NeighborsDim: 100,
	}
	data, err := bench.PrepHdf5BenchDataset(dataConfig)
	if err != nil {
		t.Fatal(err)
	}

	minStd, maxStd := bench.GetFloat64Range([][]float64{data.Std})
	t.Log("Dimensions std's range: ", minStd, maxStd)

	// NOTE: look at the ground truth distances values
	minDist, maxDist := bench.GetFloat64Range(data.Distances)
	t.Log("Ground truth distances range: ", minDist, maxDist)

	config := &bench.SearchConfig{
		Metric:        lsh.NewL2(),
		MaxNN:         10,
		MaxDist:       3000,
		MaxCandidates: 60000,
		Epsilon:       0.05,
	}
	// t.Run("NN", func(t *testing.T) {
	// 	testNearestNeighbors(t, config, data)
	// })

	config = &bench.SearchConfig{
		NDims:         784,
		BatchSize:     500,
		KMinVecs:      200,
		NTrees:        10,
		Metric:        lsh.NewL2(),
		MaxNN:         10,
		Epsilon:       0.05,
		MaxDist:       2200,
		MaxCandidates: 5000,
	}
	t.Run("LSH", func(t *testing.T) {
		testLSH(t, config, data)
	})
}

func TestEuclideanSift(t *testing.T) {
	dataConfig := &bench.BenchDataConfig{
		DatasetPath:  "../test-data/sift-128-euclidean.hdf5",
		SampleSize:   200000,
		TrainDim:     128,
		NeighborsDim: 100,
	}
	data, err := bench.PrepHdf5BenchDataset(dataConfig)
	if err != nil {
		t.Fatal(err)
	}
	minStd, maxStd := bench.GetFloat64Range([][]float64{data.Std})
	t.Log("Dimensions std's range: ", minStd, maxStd)
	minMean, maxMean := bench.GetFloat64Range([][]float64{data.Mean})
	t.Log("Dimensions mean's range: ", minMean, maxMean)

	// NOTE: look at the ground truth distances values
	minDist, maxDist := bench.GetFloat64Range(data.Distances)
	t.Log("Ground truth distances range: ", minDist, maxDist)

	config := &bench.SearchConfig{
		Metric:        lsh.NewL2(),
		MaxNN:         10,
		MaxDist:       400,
		MaxCandidates: 1000000,
		Epsilon:       0.05,
	}

	// t.Run("NN", func(t *testing.T) {
	// 	testNearestNeighbors(t, config, data)
	// })

	config = &bench.SearchConfig{
		Metric:        lsh.NewL2(),
		NDims:         128,
		BatchSize:     500,
		NTrees:        40,
		KMinVecs:      300,
		MaxNN:         10,
		MaxDist:       300,
		Epsilon:       0.05,
		MaxCandidates: 10000,
	}
	t.Run("LSH", func(t *testing.T) {
		testLSH(t, config, data)
	})
}

func TestAngularNYTimes(t *testing.T) {
	dataConfig := &bench.BenchDataConfig{
		DatasetPath:  "../test-data/nytimes-256-angular.hdf5",
		SampleSize:   60000,
		TrainDim:     256,
		NeighborsDim: 100,
	}
	data, err := bench.PrepHdf5BenchDataset(dataConfig)
	if err != nil {
		t.Fatal(err)
	}
	minStd, maxStd := bench.GetFloat64Range([][]float64{data.Std})
	t.Log("Dimensions std's range: ", minStd, maxStd)
	minMean, maxMean := bench.GetFloat64Range([][]float64{data.Mean})
	t.Log("Dimensions mean's range: ", minMean, maxMean)

	// NOTE: look at the ground truth distances values
	minDist, maxDist := bench.GetFloat64Range(data.Distances)
	t.Log("Ground truth distances range: ", minDist, maxDist)

	config := &bench.SearchConfig{
		Metric:        lsh.NewAngular(),
		MaxNN:         10,
		MaxDist:       0.85,
		MaxCandidates: 290000,
		Epsilon:       0.05,
	}
	// t.Run("NN", func(t *testing.T) {
	// 	testNearestNeighbors(t, config, data)
	// })

	config = &bench.SearchConfig{
		Metric:        lsh.NewAngular(),
		NDims:         256,
		BatchSize:     500,
		NTrees:        200,
		KMinVecs:      200,
		MaxNN:         10,
		MaxDist:       0.81,
		Epsilon:       0.05,
		MaxCandidates: 20000,
	}
	t.Run("LSH", func(t *testing.T) {
		testLSH(t, config, data)
	})
}

func TestAngularGlove(t *testing.T) {
	dataConfig := &bench.BenchDataConfig{
		DatasetPath:  "../test-data/glove-200-angular.hdf5",
		SampleSize:   200000,
		TrainDim:     200,
		NeighborsDim: 100,
	}
	data, err := bench.PrepHdf5BenchDataset(dataConfig)
	if err != nil {
		t.Fatal(err)
	}
	minStd, maxStd := bench.GetFloat64Range([][]float64{data.Std})
	t.Log("Dimensions std's range: ", minStd, maxStd)
	minMean, maxMean := bench.GetFloat64Range([][]float64{data.Mean})
	t.Log("Dimensions mean's range: ", minMean, maxMean)

	// NOTE: look at the ground truth distances values
	minDist, maxDist := bench.GetFloat64Range(data.Distances)
	t.Log("Ground truth distances range: ", minDist, maxDist)

	config := &bench.SearchConfig{
		Metric:        lsh.NewAngular(),
		MaxNN:         10,
		MaxDist:       0.75,
		MaxCandidates: 1183514,
		Epsilon:       0.05,
	}
	// t.Run("NN", func(t *testing.T) {
	// 	testNearestNeighbors(t, config, data)
	// })

	config = &bench.SearchConfig{
		Metric:        lsh.NewAngular(),
		NDims:         200,
		BatchSize:     500,
		NTrees:        150,
		KMinVecs:      300,
		MaxNN:         10,
		MaxDist:       0.75,
		Epsilon:       0.05,
		MaxCandidates: 20000,
	}
	t.Run("LSH", func(t *testing.T) {
		testLSH(t, config, data)
	})
}
