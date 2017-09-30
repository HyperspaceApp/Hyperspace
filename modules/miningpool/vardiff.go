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

func (s *Session) newVardiff() *Vardiff {
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

	s.lastVardiffRetarget = time.Now().Add(time.Duration(-retargetDuration / 2.0))
	s.lastVardiffTimestamp = time.Now()

	return v
}

func (s *Session) checkDiffOnNewShare() bool {

	sinceLast := time.Now().Sub(s.lastVardiffTimestamp).Seconds()
	s.SetLastShareDuration(sinceLast)
	s.lastVardiffTimestamp = time.Now()
	if s.log != nil {
		s.log.Debugf("Duration: %f\n", time.Now().Sub(s.lastVardiffRetarget).Seconds())
	}
	if time.Now().Sub(s.lastVardiffRetarget).Seconds() < retargetDuration {
		return false
	}
	s.lastVardiffRetarget = time.Now()

	average := s.ShareDurationAverage()
	var deltaDiff float64
	deltaDiff = float64(targetDuration) / float64(average)
	if s.log != nil {
		s.log.Debugf("Average: %f Delta %f\n", average, deltaDiff)
	}
	if average < s.vardiff.tmax && average > s.vardiff.tmin { // close enough
		return false
	}
	if s.log != nil {
		s.log.Debugf("Old difficulty was %v\n", s.currentDifficulty)
	}
	s.SetCurrentDifficulty(s.CurrentDifficulty() * deltaDiff)
	if s.log != nil {
		s.log.Printf("Set new difficulty to %v\n", s.currentDifficulty)
	}
	return true
}
