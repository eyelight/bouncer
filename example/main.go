package main

import (
	"device/arm"
	"machine"
	"time"

	"github.com/eyelight/bouncer"
)

var (
	sysTicks  = make(chan struct{}, 1)
	aliceChan = make(chan bouncer.PressLength, 1)
	bobChan   = make(chan bouncer.PressLength, 1)
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
	case sysTicks <- struct{}{}:
	default:
	}
}

func main() {
	launchSystick()
	btn, err := bouncer.New(machine.D3, aliceChan, bobChan)
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
	go reactToPresses("Alice", aliceChan)
	go reactToPresses("Bob", bobChan)
	go bouncer.Debounce(sysTicks)
	select {}
}

func reactToPresses(name string, ch chan bouncer.PressLength) {
	for {
		select {
		case pl := <-ch:
			switch pl {
			case bouncer.ShortPress:
				println(name + " got a short press")
			case bouncer.LongPress:
				println(name + " got a long press")
			case bouncer.ExtraLongPress:
				println(name + " got an extra long press")
			case bouncer.Bounce:
				println(name + " got a bounce")
			}
		}
	}
}
