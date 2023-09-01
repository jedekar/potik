package potik

import (
	"context"
	"testing"
)

func TestGraph(t *testing.T) {
	graph1 := NewGraph()

	graph1.Add("echo", &Echo[int]{})
	graph1.Add("repeater", &Repeater{})

	if err := Connect[int](graph1, "echo", "Out", "repeater", "Times"); err != nil {
		t.Fatalf(err.Error())
	}

	graph2 := NewGraph()

	graph2.Add("echo", &Echo[string]{})
	graph2.MapInPort("In", "echo", "In[main]")
	graph2.MapOutPort("Out", "echo", "Out")

	graph1.Add("subgraph", graph2)

	if err := Connect[string](graph1, "repeater", "Words", "subgraph", "In"); err != nil {
		t.Fatalf(err.Error())
	}

	graph1.MapInPort("In1", "echo", "In[main]")
	graph1.MapOutPort("Out", "subgraph", "Out")

	in1 := make(chan int)
	if err := SetInPort(graph1, "In1", in1); err != nil {
		t.Fatalf(err.Error())
	}

	if err := AddIIP(graph1, "repeater", "Word", "Abecedary"); err != nil {
		t.Fatalf(err.Error())
	}

	out := make(chan string)
	if err := SetOutPort(graph1, "Out", out); err != nil {
		t.Fatalf(err.Error())
	}

	wait := Run(context.Background(), graph1)
	go func() {
		in1 <- 1
		close(in1)
	}()

	result := <-out
	if result != "Abecedary" {
		t.Fatalf("result differs from expected value, got %+v", result)
	}

	<-wait
	t.Logf("SUCCESS\n")
}

type Echo[T any] struct {
	InMap map[string]<-chan T
	Out   chan<- T
}

func (c *Echo[T]) Process(ctx context.Context) {
	for _, i := range c.InMap {
		input := <-i
		c.Out <- input
	}
}

func (c *Echo[T]) Inputs() map[string]any {
	return map[string]any{
		"In": &c.InMap,
	}
}

func (c *Echo[T]) Outputs() map[string]any {
	return map[string]any{
		"Out": &c.Out,
	}
}

type Repeater struct {
	Word  <-chan string
	Times <-chan int

	Words chan<- string
}

func (c *Repeater) Process(ctx context.Context) {
	times := 0
	word := ""

	for c.Times != nil || c.Word != nil {
		select {
		case t, ok := <-c.Times:
			if !ok {
				c.Times = nil
				break
			}

			times = t
			c.repeat(word, times)
		case w, ok := <-c.Word:
			if !ok {
				c.Word = nil
				break
			}

			word = w
			c.repeat(word, times)
		}
	}
}

func (c *Repeater) Inputs() map[string]any {
	return map[string]any{
		"Word":  &c.Word,
		"Times": &c.Times,
	}
}

func (c *Repeater) Outputs() map[string]any {
	return map[string]any{
		"Words": &c.Words,
	}
}

func (c *Repeater) repeat(word string, times int) {
	if word == "" || times <= 0 {
		return
	}

	for i := 0; i < times; i++ {
		c.Words <- word
	}
}
