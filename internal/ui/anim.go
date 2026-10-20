package ui

import (
	"math"
	"time"
)

// Animate turns on moving backgrounds, twinkling stars and the shimmering
// logo. The screen already redraws several times a second for the spinner,
// so animation only reads the clock.
var Animate = false

// AnimSpeed scales how fast things move (1 is normal).
var AnimSpeed = 1.0

var animStart = time.Now()

// Phase is the animation clock in radians-ish units: about one unit per
// second at normal speed, and frozen at 0 when animations are off.
func Phase() float64 {
	if !Animate {
		return 0
	}
	return time.Since(animStart).Seconds() * AnimSpeed
}

// shimmer moves a gradient position t (0..1) along with the clock, bouncing
// so colors never jump.
func shimmer(t float64) float64 {
	if !Animate {
		return t
	}
	v := math.Mod(t+Phase()*0.25, 2)
	if v > 1 {
		v = 2 - v
	}
	return v
}

var noise = []rune("⣾⣽⣻⢿⡿⣟⣯⣷⠿⠶⡷⢾⣶⣤")

// Scramble shows s partly revealed: letters before p (0..1) are real, the
// rest are flickering braille noise. Spaces stay spaces.
func Scramble(s string, p float64) string {
	r := []rune(s)
	n := int(p * float64(len(r)))
	tick := int(time.Since(animStart).Milliseconds() / 45)
	for i := n; i < len(r); i++ {
		if r[i] != ' ' {
			r[i] = noise[int(hash(i, tick, 13)*1000)%len(noise)]
		}
	}
	return string(r)
}
