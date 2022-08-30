package bouncer

/*

package bouncer is a button library for recognizing button-presses of various lengths,
and implements a form of debouncing of the button

basic flow:
Button/Pin setup:
	- button mode is assumed to be InputPullup for now
	- the interrupt fires on buttonDown & buttonUp events (pinFalling & pinRising)
Interrupt Service Routine:
	- the ISR samples the pin & records the time of the pin sample, sending it to a channel
Recognizer:
	- the ISR channel is consumed by the recognizer,
	- the recognizer awaits the completion of a buttonDown->buttonUp sequence
	- each incoming bounce is evaluated for timing and ignored if short, or not the polarity we currently await
	- when a valid buttonUp time 'completes' a stored buttonDown time, the button-down duration is computed
	- the down duration is matched against values for short, long, and extra long presses
	- if the duration matches, the event is published to output channels
*/

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"machine"
)

const (
	ERROR_DEBOUNCE_TOO_SHORT  = "Debounces should be greater than 10ms"
	ERROR_DEBOUNCE_TOO_LONG   = "Debounces should be shorter than 30ms"
	ERROR_TIMES_MUST_ASCEND   = "Button press intervals must ascend in duration (sp, lp, elp)"
	ERROR_INVALID_PRESSLENGTH = "PressLength not understood"
)

type PressLength uint8

const (
	Debounce PressLength = iota
	ShortPress
	LongPress
	ExtraLongPress
)

type bounce struct {
	t time.Time // time at pin.Get()
	s bool      // output of pin.Get()
}

type button struct {
	pin              *machine.Pin
	name             string
	quiet            bool // false will spam the serial monitor
	debounceInterval time.Duration
	shortPress       time.Duration
	longPress        time.Duration
	extraLongPress   time.Duration
	isrChan          chan bounce        // channel published by the interrupt handler HandlePin & consumed by the recognizer RecognizeInput
	outChans         []chan PressLength // various channels for each subscriber of this button's events
}

type Bouncer interface {
	Configure(func(machine.Pin)) error
	HandlePin(machine.Pin)
	RecognizeAndPublish()
	Duration(PressLength) (time.Duration, error)
	SetDebounceDuration(t time.Duration) error
	SetPressDurations(sp, lp, elp time.Duration) error
	Pin() *machine.Pin
	Get() bool
	ChISR() chan bounce
	ChOut() []chan PressLength
	Name() string
	StateString() string
}

// New returns a new Bouncer with the given pin, with a 50ms debounceInterval
func New(p machine.Pin, q bool, name string, isrChan chan bounce, outs ...chan PressLength) Bouncer {
	chans := make([]chan PressLength, 0, 5)
	for i := range outs {
		chans = append(chans, outs[i])
	}
	return &button{
		pin:              &p,
		name:             name,
		quiet:            q,
		debounceInterval: 21 * time.Millisecond,
		shortPress:       22 * time.Millisecond,
		longPress:        500 * time.Millisecond,
		extraLongPress:   1971 * time.Millisecond,
		isrChan:          isrChan,
		outChans:         chans,
	}
}

// Configure sets the pin mode to InputPullup & assigns an interrupt handler to PinFalling events;
// 'isr' should probably be the inner function returned by ButtonDownFunc
func (b *button) Configure(isr func(machine.Pin)) error {
	b.pin.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	err := b.pin.SetInterrupt(machine.PinFalling|machine.PinRising, isr)
	if err != nil {
		return err
	}
	if !b.quiet {
		println("	Debounce: " + b.debounceInterval.String())
		println("	ShortPress: " + b.shortPress.String())
		println("	LongPress: " + b.longPress.String())
		println("	ExtraLongPress: " + b.extraLongPress.String())
	}
	return nil
}

// HandlePin should be an interrupt handler;
// pushes state & time to a channel,
// and it is up to the consumer to make sense of bounces
func (b *button) HandlePin(machine.Pin) {
	b.isrChan <- bounce{t: time.Now(), s: b.pin.Get()}
}

// RecognizeAndPublish should be a goroutine;
// reads pin state & sample time from channel,
// awaits completion of a buttonDown -> buttonUp sequence,
// recognizes press length,
// publishes the recognized press event to the button's output channel(s)
func (b *button) RecognizeAndPublish() {
	if !b.quiet {
		println("RecognizeAndPublish spawned...")
	}
	awaitingCompletion := true
	btnDown := time.Time{}
	btnUp := time.Time{}
	for {
		select {
		case tr := <-b.isrChan:
			switch tr.s {
			case false: // button is 'down'
				if !awaitingCompletion { // if we were awaitng a new bounce sequence to begin
					awaitingCompletion = true // let's look for 'up' signals now
					btnDown = tr.t            // set the received time as the beginning of the sequence
					break
				} else { // if we were awaiting the conclusion of a bounce sequence
					break // ignore 'down' signal & reset the loop
				}
			case true: // button is 'up'
				if !awaitingCompletion { // if we were awaiting a new bounce sequence
					break // ignore 'up' signals
				} else { // if we were awaiting the conclusion of a bounce sequence
					if tr.t.Sub(btnDown) > b.debounceInterval { // if the interval between down & up is greater than debounceInterval
						awaitingCompletion = false // let's look for 'down' signals now
						btnUp = tr.t               // set the received tim as the end of the sequence
					}
				}
			}

			// calculate button-down duration
			dur := btnUp.Sub(btnDown)
			if !b.quiet {
				println("Down duration " + dur.String())
			}

			// Recognize & publish to channel(s)
			if dur >= b.extraLongPress {
				for i := range b.outChans {
					b.outChans[i] <- ExtraLongPress
				}
				if !b.quiet {
					println("	sent ExtraLongPress")
				}
				break
			} else if dur < b.extraLongPress && dur >= b.longPress {
				for i := range b.outChans {
					b.outChans[i] <- LongPress
				}
				if !b.quiet {
					println("	sent LongPress")
				}
				break
			} else if dur < b.longPress && dur >= b.shortPress {
				for i := range b.outChans {
					b.outChans[i] <- ShortPress
				}
				if !b.quiet {
					println("	sent ShortPress")
				}
				break
			} else {
				if !b.quiet {
					println("button down duration shorter than shortPress; no action taken")
				}
			}
		default:
		}
	}
}

// SetDebounceInterval overwrites the button's debounceInterval field with the passed-in duration
func (b *button) SetDebounceDuration(t time.Duration) error {
	if t < 10*time.Millisecond {
		return errors.New(ERROR_DEBOUNCE_TOO_SHORT)
	}
	if t > 30*time.Millisecond {
		return errors.New(ERROR_DEBOUNCE_TOO_LONG)
	}
	b.debounceInterval = t
	return nil
}

// SetIntervals overwrites the button's fields shortPress, longPress, extraLongPress with passed-in durations
// the longest duration of these which is exceeded by a buttonPress will be sent to subscribers by the handler
func (b *button) SetPressDurations(sp, lp, elp time.Duration) error {
	if sp > lp || sp > elp || lp > elp {
		return errors.New(ERROR_TIMES_MUST_ASCEND)
	}
	b.shortPress = sp
	b.longPress = lp
	b.extraLongPress = elp
	return nil
}

// Duration reutrns the duration used by the passed-in PressLength
func (b *button) Duration(l PressLength) (time.Duration, error) {
	switch l {
	case Debounce:
		return b.debounceInterval, nil
	case ShortPress:
		return b.shortPress, nil
	case LongPress:
		return b.longPress, nil
	case ExtraLongPress:
		return b.extraLongPress, nil
	}
	return time.Duration(0), errors.New(ERROR_INVALID_PRESSLENGTH)
}

// Pin returns the pin assigned to the button
func (b *button) Pin() *machine.Pin {
	return b.pin
}

// Get reads the pin and returns as true=high/false=low
func (b *button) Get() bool {
	return b.pin.Get()
}

// ChISR returns the channel written by the ISR
func (b *button) ChISR() chan bounce {
	return b.isrChan
}

// ChOut returns the first channel published by RecognizeAndPublish
func (b *button) ChOut() []chan PressLength {
	return b.outChans
}

/*
	Statist interface methods
		- Name() string
		- StateString() string
*/

// Name returns the name of this button
func (b *button) Name() string {
	return b.name
}

// StateString returns a string meaningful to the state of this button
func (b *button) StateString() string {
	st := strings.Builder{}
	st.Grow(150)
	st.WriteString(b.name)
	st.WriteString(" - (Bouncer): ")
	st.WriteString(strconv.FormatBool(b.pin.Get()))
	st.WriteByte(9)  // newline
	st.WriteByte(10) // tab
	st.WriteString("Debounce Duration: ")
	st.WriteString(b.debounceInterval.String())
	st.WriteByte(9)
	st.WriteByte(10)
	st.WriteString("Short Press Duration: ")
	st.WriteString(b.shortPress.String())
	st.WriteByte(9)
	st.WriteByte(10)
	st.WriteString("Long Press Duration: ")
	st.WriteString(b.longPress.String())
	st.WriteByte(9)
	st.WriteByte(10)
	st.WriteString("Extra Long Press Duration: ")
	st.WriteString(b.extraLongPress.String())
	return st.String()
}
