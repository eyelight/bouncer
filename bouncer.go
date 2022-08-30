package bouncer

/*

package bouncer is a button library for recognizing button-presses of various lengths,
and implements a form of debouncing of the button

basic flow:
Button/Pin setup:
	- button mode is assumed to be InputPullup for now
	- the interrupt fires on buttonDown & buttonUp events (pinFalling & pinRising)
Interrupt Service Routine -> HandlePin()
	- attach this to a pin as an interrupt handler
	- the ISR samples the pin & records the time of the pin sample, sending it to a channel
Recognizer -> RecognizeAndPublish()
	- run this as a goroutine
	- the ISR channel is consumed here,
	- the recognizer awaits the completion of a buttonDown->buttonUp sequence
	- each incoming bounce is evaluated for timing and ignored if not the polarity we currently await, or too short
	- after the first buttonUp time that 'completes' the current buttonDown time, the button-down duration is computed
	- the down duration is matched against values for short, long, and extra long presses
	- if the duration matches, the button press length signal is published to output channels
*/

import (
	"errors"
	"strings"
	"time"

	"machine"
)

const (
	ERROR_DEBOUNCE_TOO_SHORT  = "Debounces should probably be greater than 10ms"
	ERROR_DEBOUNCE_TOO_LONG   = "Debounces should probably be shorter than 30ms"
	ERROR_TIMES_MUST_ASCEND   = "Button press intervals must ascend in duration (sp, lp, elp)"
	ERROR_INVALID_PRESSLENGTH = "PressLength not understood"
	ERROR_NO_OUTPUT_CHANNELS  = "New bouncer wasn't given any output channels"
	REPORT_EXTRA_LONG_PRESS   = "	sent ExtraLongPress"
	REPORT_LONG_PRESS         = "	sent LongPress"
	REPORT_SHORT_PRESS        = "	sent ShortPress"
	REPORT_TOO_SHORT          = "	duration between bounce & short press; no action taken"
	REPORT_BOUNCE             = "	bounce detected; no action taken"
)

type PressLength uint8

const (
	Debounce PressLength = iota
	ShortPress
	LongPress
	ExtraLongPress
)

type Bounce struct {
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
	isrChan          chan Bounce        // channel published by the interrupt handler HandlePin & consumed by the recognizer RecognizeInput
	outChans         []chan PressLength // various channels for each subscriber of this button's events
}

type Bouncer interface {
	Configure() error
	HandlePin(machine.Pin)
	RecognizeAndPublish(tickerCh chan struct{})
	Duration(PressLength) (time.Duration, error)
	SetDebounceDuration(t time.Duration) error
	SetPressDurations(sp, lp, elp time.Duration) error
	Pin() *machine.Pin
	Get() bool
	ChISR() chan Bounce
	ChOut() []chan PressLength
	Name() string
	StateString() string
}

// New returns a new Bouncer (or error) with the given pin, name & channels, with default intervals for
// debounce, shortPress, longPress, extraLongPress; passing 'q' as false will spam the serial monitor
func New(p machine.Pin, q bool, name string, isrChan chan Bounce, outs ...chan PressLength) (Bouncer, error) {
	if len(outs) < 1 {
		return nil, errors.New(ERROR_NO_OUTPUT_CHANNELS)
	}
	outChans := make([]chan PressLength, 0)
	for i := range outs {
		outChans = append(outChans, outs[i])
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
		outChans:         outChans,
	}, nil
}

// Configure sets the pin mode to InputPullup & assigns an interrupt handler to PinFalling events;
// 'isr' should probably be the inner function returned by ButtonDownFunc
func (b *button) Configure() error {
	b.pin.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	err := b.pin.SetInterrupt(machine.PinFalling|machine.PinRising, b.HandlePin)
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
	b.isrChan <- Bounce{t: time.Now(), s: b.pin.Get()}
}

// RecognizeAndPublish should be a goroutine; assumes pin is of mode InputPullup so 'false' is button=down
// reads pin state & sample time from channel,
// awaits completion of a buttonDown -> buttonUp sequence,
// recognizes press length,
// publishes the recognized press event to the button's output channel(s)
func (b *button) RecognizeAndPublish(tickerCh chan struct{}) {
	if !b.quiet {
		println("RecognizeAndPublish spawned...")
	}
	ticks := 0
	btnDown := time.Time{}
	dur := btnDown.Sub(btnDown) // initial duration zero
	for {
		select {
		case <-tickerCh:
			println("tick")
			if ticks > 0 {
				ticks++
				// println(strconv.FormatInt(int64(ticks), 10) + "ticks")
			}
		case tr := <-b.isrChan:
			switch tr.s {
			case false: // button is 'down'
				if ticks == 0 { // if we were awaitng a new bounce sequence to begin
					ticks = 1
					btnDown = tr.t // set the received time as the beginning of the sequence
					continue
				} else { // if we were awaiting the conclusion of a bounce sequence
					continue // ignore 'down' signal & reset the loop
				}
			case true: // button is 'up'
				if ticks == 0 { // if we were awaiting a new bounce sequence to begin
					continue // ignore 'up' signals until there is a down
				} else { // if we were awaiting the conclusion of a bounce sequence
					if ticks >= 2 { // if the interval between down & up is greater than debounceInterval
						dur = tr.t.Sub(btnDown) // use received 'up' time to calculate sequence duration
						btnDown = time.Time{}   // reset button down time
						ticks = 0               // let's look for 'down' signals now
					} else { // if debounce interval was not exceeded, wait for next button 'up'
						continue
					}
				}
			}

			// break out of this if there's a bounce
			if dur < b.debounceInterval {
				if !b.quiet {
					println(REPORT_BOUNCE)
				}
				continue
			} else {
				if !b.quiet {
					println("Down duration " + dur.String())
				}
				// Recognize & publish to channel(s)
				if dur >= b.extraLongPress { // duration was extraLongPress
					if !b.quiet {
						println(REPORT_EXTRA_LONG_PRESS)
					}
					b.publish(ExtraLongPress)
				} else if dur < b.extraLongPress && dur >= b.longPress { // duration was longPress
					if !b.quiet {
						println(REPORT_LONG_PRESS)
					}
					b.publish(LongPress)
				} else if dur < b.longPress && dur >= b.shortPress { // duration was shortPress
					if !b.quiet {
						println(REPORT_SHORT_PRESS)
					}
					b.publish(ShortPress)
				}
			}
		default: // don't block
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
func (b *button) ChISR() chan Bounce {
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
	st.WriteByte(9) // tab
	st.WriteString("Debounce Duration: ")
	st.WriteString(b.debounceInterval.String())
	st.WriteByte(9)
	st.WriteString("Short Press Duration: ")
	st.WriteString(b.shortPress.String())
	st.WriteByte(9)
	st.WriteString("Long Press Duration: ")
	st.WriteString(b.longPress.String())
	st.WriteByte(9)
	st.WriteString("Extra Long Press Duration: ")
	st.WriteString(b.extraLongPress.String())
	return st.String()
}

func (b *button) publish(p PressLength) {
	for i := range b.outChans {
		b.outChans[i] <- p
	}
}
