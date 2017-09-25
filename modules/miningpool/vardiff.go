package pool

import (
	"time"
)

const (
	targetDuration   = float64(4)  // targeted seconds between shares
	retargetDuration = float64(90) // how often do we consider changing difficulty
	variancePercent  = 25          // how much we let the share duration vary between retargetting
)

type Vardiff struct {
	tmax    float64
	tmin    float64
	bufSize uint64
}

func (w *Worker) newVardiff() *Vardiff {
	variance := float64(targetDuration) * (float64(variancePercent) / 100.0)
	size := uint64(retargetDuration / targetDuration * 4)
	if size > numSharesToAverage {
		size = numSharesToAverage
	}
	v := &Vardiff{
		tmax:    targetDuration + variance,
		tmin:    targetDuration - variance,
		bufSize: numSharesToAverage,
	}

	w.lastVardiffRetarget = time.Now().Add(time.Duration(-retargetDuration / 2.0))
	w.lastVardiffTimestamp = time.Now()

	return v
}

func (w *Worker) checkDiffOnNewShare() bool {

	sinceLast := time.Now().Sub(w.lastVardiffTimestamp).Seconds()
	w.SetLastShareDuration(sinceLast)
	w.lastVardiffTimestamp = time.Now()
	w.log.Debugf("Duration: %f\n", time.Now().Sub(w.lastVardiffRetarget).Seconds())
	if time.Now().Sub(w.lastVardiffRetarget).Seconds() < retargetDuration {
		return false
	}
	w.lastVardiffRetarget = time.Now()

	average := w.ShareDurationAverage()
	var deltaDiff float64
	deltaDiff = float64(targetDuration) / float64(average)
	w.log.Debugf("Average: %f Delta %f\n", average, deltaDiff)
	if average < w.vardiff.tmax && average > w.vardiff.tmin { // close enough
		return false
	}
	w.log.Debugf("Old difficulty was %v\n", w.currentDifficulty)
	w.SetCurrentDifficulty(w.CurrentDifficulty() * deltaDiff)
	w.log.Printf("Set new difficulty to %v\n", w.currentDifficulty)
	return true
}
