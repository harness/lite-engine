package testsplitter

import (
	"encoding/json"
	"sort"

	"github.com/bmatcuk/doublestar/v4"
)

// ProcessFiles removes non-existent files and adds new files to the file-times map.
func ProcessFiles(fileTimesMap map[string]float64, currentFileSet map[string]bool, defaultTime float64) {
	// First Remove the entries that no longer exist in the filesystem.
	for file := range fileTimesMap {
		if !currentFileSet[file] {
			delete(fileTimesMap, file)
		}
	}

	// For files that don't have time data, use the average value.
	averageFileTime := 0.0
	if len(fileTimesMap) > 0 { // To avoid divide-by-zero error
		for _, time := range fileTimesMap {
			averageFileTime += time
		}
		averageFileTime /= float64(len(fileTimesMap))
	} else {
		averageFileTime = defaultTime
	}

	// Populate the file time for missing files.
	for file := range currentFileSet {
		if _, isSet := fileTimesMap[file]; isSet {
			continue
		}
		fileTimesMap[file] = averageFileTime
	}
}

func GetTestData(patterns, excludePatterns []string) (map[string]bool, error) {
	currentFileSet := make(map[string]bool)

	// We are not using filepath.Glob,
	// because it doesn't support '**' (to match all files in all nested directories)
	for _, pattern := range patterns {
		currentFiles, err := doublestar.FilepathGlob(pattern)
		if err != nil {
			return currentFileSet, err
		}

		for _, file := range currentFiles {
			currentFileSet[file] = true
		}
	}

	// Exclude the specified files
	for _, excludePattern := range excludePatterns {
		excludedFiles, err := doublestar.FilepathGlob(excludePattern)
		if err != nil {
			return currentFileSet, err
		}
		for _, file := range excludedFiles {
			delete(currentFileSet, file)
		}
	}
	return currentFileSet, nil
}

func ConvertMap(intMap map[string]int) map[string]float64 {
	fileTimesMap := make(map[string]float64)
	for k, v := range intMap {
		fileTimesMap[k] = float64(v)
	}
	return fileTimesMap
}

func ConvertMapToJSON(timeMap map[string]float64) []byte {
	timeMapJSON, _ := json.Marshal(timeMap)
	return timeMapJSON
}

// SplitFiles splits files based on the provided timing data. The output is a list of
// buckets/splits for files as well as the duration of each bucket.
// Args:
//	 fileTimesMap: Map containing the time data: <fileName, Duration>
//	 splitTotal: Desired number of splits
// Returns:
//	 List of buckets with filenames. Eg: [ ["file1", "file2"], ["file3"], ["file4", "file5"]]
//	 List of bucket durations. Eg: [10.4, 9.3, 10.5]

func SplitFiles(fileTimesMap map[string]float64, splitTotal int) ([][]string, []float64) { //nolint:gocritic
	buckets := make([][]string, splitTotal)
	bucketTimes := make([]float64, splitTotal)

	// Build a sorted list of files
	fileTimes := make(fileTimesList, 0)
	for file, time := range fileTimesMap {
		fileTimes = append(fileTimes, fileTimesListItem{file, time})
	}
	sort.Sort(fileTimes)

	for _, file := range fileTimes {
		// find bucket with min weight
		minBucket := 0
		for bucket := 1; bucket < splitTotal; bucket++ {
			if bucketTimes[bucket] < bucketTimes[minBucket] {
				minBucket = bucket
			}
		}
		// add file to bucket
		buckets[minBucket] = append(buckets[minBucket], file.name)
		bucketTimes[minBucket] += file.time
	}

	return buckets, bucketTimes
}
