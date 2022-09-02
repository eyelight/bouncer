# Bouncer 
## Bouncer is a button input handler package. It supports systick-based debouncing of button input and recognizes button-press-lengths of different durations

Bouncer assumes you are using long-lived goroutines which listen for updates on a long-lived channel. At the end of a buttonDown -> buttonUp sequence, a Bouncer recognizes the duration of the press and sends this information to interested subscribers. When setting up your Bouncer(s), you can add an output channel for each of your interested subscribers.

## `New`
Pass an unconfigured pin here. Configure will reconfigure it to InputPullup anyway. 
The channels each Bouncer needs are as follows:
- `tickerCh` – one channel on which to receive tick events from our SysTick_Handler relay `RelayTicks`
- `isrChan` – one channel on which to receive buttonDown/buttonUp events, Bounces, from the interrupt handler
- `...outs` – any number of channels on which to publish button recognition events as a PressLength

## `Configure`
A custom duration for short, long, & extra long presses can be set in a `BouncerConfig` struct. To override default values, pass this to Configure, or pass an empty `BouncerConfig` to keep default values. The bouncer's pin is set to InputPullup

In `Configure`, a function becomes the button's pin interrupt handler:
  - fires on `PinRising` & `PinFalling` events
  - it sends a `Bounce` to the Bouncer's `isrChan` channel, which is consumed by `RecognizeAndPublish`
  - a `Bounce` contains a `bool` and a `time.Time` set to the pin's state and the time at which it was registered 

## `RecognizeAndPublish` 

This is the button-press-length recognizer & publisher, and should be run as a long-lived goroutine.
- The function blocks on communication from one of two channels, either the Bouncer's `tickerCh` which means a new tick was sent from the `SysTick_Handler`, or the Bouncer's `isrChan` which means a new button up/down interrupt event was received
- Initially, `RecognizeAndPublish` is looking for a buttonDown event, and will ignore both systicks & buttonUp interrupts. 
- After the first buttonDown event arrives, the received time is noted for later evaluation, and the function begins to increment `ticks` whenever a SysTick is received on `tickerCh`. 
- At this point, the function begins to expect buttonUp events, so, buttonDown interrupts are ignored. 
- Upon the first buttonUp event, the received time is subtracted from the buttonDown time, resulting in a buttonDown duration. This duration is compared to the set of `PressLength` durations, thereby becoming recognized. The resulting `PressLength` is broadcast to all output channels

## Some plumbing in `main` to set up your SysTick_Handler
A systick is a machine level event to which we can attach our own handler. Since this is global in nature, it doesn't belong in this package; you must set up a "SysTick_Handler" yourself and allow your Bouncer to consume its channel, indirectly through a relay in order to fan-out the ticks to multiple bouncers.

## First, set up the timer

In your `init` or `main`, set up a timer set to an interval of your desired debounce duration. The following will fire 10 times per second, obtaining a ~100ms debounce threshold; a higher denominator (shorter duration) may be more appropriate, perhaps 40 -> 25ms. It's up to you. 

```golang
func launchSystick() {
    err := arm.SetupSystemTimer(machine.CPUFrequency() / 10)
    if err != nil {
        println("error launching systick timer!")
    }
    println("launched systick timer...")
}
```

## Next, our systick handling function

SysTick_Handler is attached to a function using the go macro `//go:export SysTick_Handler`. The function should simply `select` and send empty structs to a buffered struct channel (capacity of 1). 

One interesting thing about this is that you never need to call it; you only define it and the macro will call it under the hood for you – as many times per second as the call to `arm.SetupSystemTimer` above.

```golang
//go:export SysTick_Handler
func handleSystick() {
    select {
    case tickCh <- struct{}{}:
    default:
    }
}
```

## `RelayTicks` – Handling Multiple Bouncers

Since there is only one systick handler, we want to consume `tickCh` with another function which will fan out the tick across multiple interested channels. 

This is done internally by the package – simply call the package-level function `RelayTicks` as a goroutine and pass it the same channel `tickCh` produced by our systick handler. Do not consume `tickCh` in more than 1 place.