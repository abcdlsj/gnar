package metrics

import "fmt"

type Traffic struct {
	upwardBytes, downwardBytes int64
	st, et                     int64
}

func CalculateBandwidth(traffics []Traffic) (string, string, string) {
	upBytes := int64(0)
	dnBytes := int64(0)
	sumElapsedNano := int64(0)
	for _, t := range traffics {
		upBytes += t.upwardBytes
		dnBytes += t.downwardBytes
		sumElapsedNano += t.et - t.st
	}

	avgElapsedTime := float64(sumElapsedNano) / float64(len(traffics)) // nanosecond
	upbw := float64(upBytes) / avgElapsedTime * 1e9
	dnbw := float64(dnBytes) / avgElapsedTime * 1e9

	return humanBytes(upbw) + "/s", humanBytes(dnbw) + "/s",
		humanBytes(float64(upBytes) + float64(dnBytes))
}

func humanBytes(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for b > 1024 {
		b /= 1024
		i++
	}
	return fmt.Sprintf("%.2f%s", b, units[i])
}
