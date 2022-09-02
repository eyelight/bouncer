package main

import (
	"device/arm"
	"machine"
	"time"

	"github.com/eyelight/bouncer"
)

var (
	systickCh = make(chan struct{}, 1)
	outChan   = make(chan bouncer.PressLength, 1)
)

func launchSystick() {
	err := arm.SetupSystemTimer(machine.CPUFrequency() / 40)
	if err != nil {
		println("error launching systick timer")
	}
}

//go:export SysTick_Handler
func handleSystick() {
	select {
	case systickCh <- struct{}{}:
	default:
	}
}

func main() {
	launchSystick()
	btn, err := bouncer.New(machine.D2, outChan)
	if err != nil {
		println("couldn't make new bouncer")
	}
	err = btn.Configure(bouncer.Config{
		Short:     18 * time.Millisecond,
		Long:      550 * time.Millisecond,
		ExtraLong: 1500 * time.Millisecond,
	})
	if err != nil {
		println(err)
	}

	go btn.RecognizeAndPublish()
	go reactToPresses(outChan)
	go bouncer.RelayTicks(systickCh)
	select {}
}

func reactToPresses(ch chan bouncer.PressLength) {
	for {
		select {
		case pl := <-ch:
			switch pl {
			case bouncer.ShortPress:
				println("I got a short press")
			case bouncer.LongPress:
				println("I got a long press")
			case bouncer.ExtraLongPress:
				println("I got an extra long press")
			case bouncer.Debounce:
				println("I got a bounce")
			}
		}
	}
}
