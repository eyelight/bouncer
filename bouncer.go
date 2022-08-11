package bouncer

/*

package bouncer is a button library implementing debouncing,
and sending & receiving events related to button presses of various lengths

the basic flow:
Button/Pin setup:
	- button mode is assumed to be InputPullup for now, so the interrupt sends buttonDown events only
	- the pin's interrupt is set to PinFalling
Interrupt Service Routine:
	- the isr ignores repeated input until a button.debounceInterval duration is met
	- isr sends debounced buttonDown events as 'false' on a bool channel for external evaluation
Button Handler:
	- button handler (ButtonDownFunc's inner function) receives debounced buttonDown events
	- the pin is sampled continuously and a timer is incremented continuously
	- when the pin's state becomes 'true' (eg, buttonUp) the timer is stopped
	- the "down duration" is computed
	- the down duration is compared to a set of PressLength durations
	- event notification is sent to an int channel with a value of PressLength
You:
	- your interested consumers can do meaningful things
*/

import (
	"errors"
	"time"

	"machine"
)

type PressLength uint8

const (
	ShortPress PressLength = iota
	LongPress
	ExtraLongPress
)

type button struct {
	pin              *machine.Pin
	debounceInterval time.Duration
	shortPress       time.Duration
	longPress        time.Duration
	extraLongPress   time.Duration
}

type Bouncer interface {
	Configure(func(machine.Pin)) error
	SetDebounceInterval(time.Duration)
	SetIntervals(sp, lp, elp time.Duration) error
	ButtonDownFunc(chan<- time.Time, *machine.Pin) func(machine.Pin)
	HandleInput(in <-chan time.Time, out1, out2 chan<- PressLength)
	Pin() *machine.Pin
}

// New returns a new Bouncer with the given pin, with a 50ms debounceInterval
func New(p machine.Pin) Bouncer {
	return &button{
		pin:              &p,
		debounceInterval: 25 * time.Millisecond,
		shortPress:       30 * time.Millisecond,
		longPress:        750 * time.Millisecond,
		extraLongPress:   1951 * time.Millisecond,
	}
}

func (b *button) Pin() *machine.Pin {
	return b.pin
}

// SetIntervals overwrites the button's fields shortPress, longPress, extraLongPress with passed-in durations
// the longest duration of these which is exceeded by a buttonPress will be sent to subscribers by the handler
func (b *button) SetIntervals(sp, lp, elp time.Duration) error {
	if sp > lp || sp > elp || lp > elp {
		return errors.New("bouncer - couldn't set intervals: sp, lp, and elp must be in ascending order")
	}
	b.shortPress = sp
	b.longPress = lp
	b.extraLongPress = elp
	return nil
}

// Configure sets the pin mode to InputPullup & assigns an interrupt handler to PinFalling events
func (b *button) Configure(isr func(machine.Pin)) error {
	b.pin.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	err := b.pin.SetInterrupt(machine.PinFalling, isr)
	println("Debounce: " + b.debounceInterval.String())
	println("ShortPress: " + b.shortPress.String())
	println("LongPress: " + b.longPress.String())
	println("ExtraLongPress: " + b.extraLongPress.String())
	if err != nil {
		return errors.New("bouncer - could not set interrupt")
	}
	return nil
}

// ButtonDownFunc returns a function designed to be passed to Configure as the 'isr' param
// channel 'ch' needs to be monitored by
func (b *button) ButtonDownFunc(ch chan<- time.Time, p *machine.Pin) func(machine.Pin) {
	println("ButtonDownFunc")
	// lastEvent := time.Now()
	return func(machine.Pin) { // the inner function sends bools and resets the timer
		now := time.Now()
		// if time.Now().Sub(lastEvent) > b.debounceInterval { // ignore 'bounces' until after b.debounceInterval
		if b.pin.Get() == false {
			ch <- now
		}
		// lastEvent = time.Now()
		// }
	}
}

// HandleInput reads from channel in and writes to channel out
func (b *button) HandleInput(in <-chan time.Time, out1, out2 chan<- PressLength) {
	for {
		select {
		case btnDown := <-in:
			// btnDown := time.Now()
			println("HandleInput -> buttonDOWN!")
			btnUp := time.Now()
			for s := b.pin.Get(); s == false; btnUp = time.Now() { // increment a timer for as long as the button is down
				s = b.pin.Get()
			} // continue when the pin reads 'true'
			println("HandleInput -> buttonUP!")
			if b.pin.Get() == true {
				dur := btnUp.Sub(btnDown)
				println("Down duration:" + dur.String())
				if dur > b.extraLongPress {
					out1 <- ExtraLongPress
					out2 <- ExtraLongPress
				} else if dur > b.longPress {
					out1 <- LongPress
					out2 <- LongPress
				} else if dur > b.shortPress {
					out1 <- ShortPress
					out2 <- ShortPress
				} else {
					println("button down duration shorter than shortPress; no action taken")
				}
			}
		default:
		}
	}
}

// SetDebounceInterval overwrites the button's debounceInterval field with the passed-in duration
func (b *button) SetDebounceInterval(t time.Duration) {
	b.debounceInterval = t
}
