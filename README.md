# ПОТІК
## Description
Potik is a Go framework for dataflow programming. 
It is modeled after a flow-based programming library called [goflow](https://github.com/trustmaster/goflow),
but removes the features required for FBP support, providing the only baseline functionality which allows 
you to structure your programs as graphs.

It also replaces Goflow's usage of reflection with Go's 1.19 generics, which supposed to reduce the amount
of mistakes occurring when program's inputs/outputs are wired up.
No runtime modifications of graphs are expected, although it *might* be possible to implement.

Potik is designed in the "truly reusable code" fashion. It's a single-file library that
you can either include as a Go module, or simply copy-paste into your project.

## Example
This is the example code adapted from Goflow's readme:
```go
package main

import (
	"context"
	"fmt"

	"github.com/jedekar/potik"
)

// Greeter sends greetings
type Greeter struct {
	Name <-chan string // input port
	Res  chan<- string // output port
}

// Process incoming data
func (c *Greeter) Process(_ context.Context) {
	// Keep reading incoming packets
	for name := range c.Name {
		greeting := fmt.Sprintf("Hello, %s!", name)
		// Send the greeting to the output port
		c.Res <- greeting
	}
}

func (c *Greeter) Inputs() map[string]any {
	return map[string]any{
		"Name": &c.Name,
	}
}

func (c *Greeter) Outputs() map[string]any {
	return map[string]any{
		"Res": &c.Res,
	}
}

// Printer prints its input on screen
type Printer struct {
	Line <-chan string // inport
}

// Process prints a line when it gets it
func (c *Printer) Process(_ context.Context) {
	for line := range c.Line {
		fmt.Println(line)
	}
}

func (c *Printer) Inputs() map[string]any {
	return map[string]any{
		"Line": &c.Line,
	}
}

func (c *Printer) Outputs() map[string]any {
	return map[string]any{}
}

// NewGreetingApp defines the app graph
func NewGreetingApp() *potik.Graph {
	n := potik.NewGraph()
	// Add processes to the network
	n.Add("greeter", &Greeter{})
	n.Add("printer", &Printer{})
	// Connect them with a channel
	potik.Connect[string](n, "greeter", "Res", "printer", "Line")
	// Our net has 1 inport mapped to greeter.Name
	n.MapInPort("In", "greeter", "Name")

	return n
}

func main() {
	// Create the network
	net := NewGreetingApp()
	// We need a channel to talk the network
	in := make(chan string)
	potik.SetInPort[string](net, "In", in)
	// Run the net
	wait := potik.Run(context.TODO(), net)
	// Now we can send some names and see what happens
	in <- "John"
	in <- "Boris"
	in <- "Hanna"
	// Send end of input
	close(in)
	// Wait until the net has completed its job
	<-wait
}
```