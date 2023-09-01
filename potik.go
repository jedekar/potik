package potik

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

type Component interface {
	Process(ctx context.Context)
	// Inputs is a map of input names to their physical representations (for components) or virtual mappings (for graphs).
	// Physical representations are: chan Type, []chan Type, or map[string]chan Type (receive-only/bidirectional).
	// Virtual mapping is a string in format "component.port" which points to the input of a component/graph inside the graph.
	Inputs() map[string]any
	// Outputs is a map of output names to their physical representations (for components) or virtual mappings (for graphs).
	// Physical representations are: chan Type, []chan Type, or map[string]chan Type (send-only/bidirectional)
	// Virtual mapping is a string in format "component.port" which points to the output of a component/graph inside the graph.
	Outputs() map[string]any
}

// Done notifies that the process is finished.
type Done struct{}

// Wait is a channel signaling of a completion.
type Wait chan Done

// Run the component process.
func Run(ctx context.Context, c Component) Wait {
	wait := make(Wait)

	go func() {
		c.Process(ctx)
		wait <- Done{}
	}()

	return wait
}

type InitialDataProviderFunc func(g *Graph)

type ChanCloseHandlerFunc func(g *Graph)

func NewChanCloseHandlerFunc[T any](channel chan T) ChanCloseHandlerFunc {
	return func(g *Graph) {
		g.chanSendersLock.Lock()
		defer g.chanSendersLock.Unlock()

		_, ok := g.ChanOpenSenders[channel]
		if !ok {
			close(channel)
			return
		}

		g.ChanOpenSenders[channel]--
		if g.ChanOpenSenders[channel] == 0 {
			close(channel)
		}
	}
}

type Graph struct {
	InputsMap  map[string]any
	OutputsMap map[string]any
	Components map[string]Component
	// Connections stores mapping of "component.port" strings to channels
	Connections          map[string]any
	InitialDataProviders []InitialDataProviderFunc
	ChanSenders          map[any][]string
	ChanReceivers        map[any][]string
	ChanOpenSenders      map[any]int
	ChanCloseHandlersMap map[string]ChanCloseHandlerFunc

	chanSendersLock sync.Locker
	waitGrp         *sync.WaitGroup
}

func NewGraph() *Graph {
	return &Graph{
		InputsMap:            make(map[string]any),
		OutputsMap:           make(map[string]any),
		Components:           make(map[string]Component),
		Connections:          make(map[string]any),
		ChanSenders:          make(map[any][]string),
		ChanReceivers:        make(map[any][]string),
		ChanOpenSenders:      make(map[any]int),
		ChanCloseHandlersMap: make(map[string]ChanCloseHandlerFunc),
		chanSendersLock:      &sync.Mutex{},
		waitGrp:              &sync.WaitGroup{},
	}
}

func (g *Graph) Process(ctx context.Context) {
	for _, p := range g.InitialDataProviders {
		go p(g)
	}

	for n, c := range g.Components {
		g.waitGrp.Add(1)

		w := Run(ctx, c)

		n := n
		go func() {
			<-w
			if closeHandler, ok := g.ChanCloseHandlersMap[n]; ok {
				closeHandler(g)
			}

			g.waitGrp.Done()
		}()
	}

	g.waitGrp.Wait()
}

func (g *Graph) Add(name string, component Component) {
	g.Components[name] = component
}

func (g *Graph) Inputs() map[string]any {
	return g.InputsMap
}

func (g *Graph) Outputs() map[string]any {
	return g.OutputsMap
}

func (g *Graph) MapInPort(inputName, cName, cInName string) {
	g.InputsMap[inputName] = fmt.Sprintf("%s.%s", cName, cInName)
}

func (g *Graph) MapOutPort(outputName, cName, cOutName string) {
	g.OutputsMap[outputName] = fmt.Sprintf("%s.%s", cName, cOutName)
}

func SetInPort[T any](c Component, inputName string, channel chan T) error {
	inputValue, accessor, err := GetInputValue(c, inputName)
	if err != nil {
		return err
	}

	switch inputValue.(type) {
	case *<-chan T:
		i := inputValue.(*<-chan T)
		*i = channel
	case *[]<-chan T:
		length := *accessor.Index + 1
		i := inputValue.(*[]<-chan T)
		if len(*i) < length {
			*i = append(*i, make(<-chan T, length-len(*i)))
		}

		(*i)[*accessor.Index] = channel
	case *map[string]<-chan T:
		i := inputValue.(*map[string]<-chan T)
		if *i == nil {
			*i = make(map[string]<-chan T)
		}

		(*i)[*accessor.Key] = channel
	default:
		return fmt.Errorf("component declared invalid input of type %T, expected one of these: %T, %T, %T", inputValue, new(<-chan T), new([]<-chan T), new(map[string]<-chan T))
	}

	return nil
}

func SetOutPort[T any](c Component, outputName string, channel chan T) error {
	outputValue, accessor, err := GetOutputValue(c, outputName)
	if err != nil {
		return err
	}

	switch outputValue.(type) {
	case *chan<- T:
		i := outputValue.(*chan<- T)
		*i = channel
	case *[]chan<- T:
		length := *accessor.Index + 1
		i := outputValue.(*[]chan<- T)
		if len(*i) < length {
			*i = append(*i, make(chan<- T, length-len(*i)))
		}

		(*i)[*accessor.Index] = channel
	case *map[string]chan<- T:
		i := outputValue.(*map[string]chan<- T)
		if *i == nil {
			*i = make(map[string]chan<- T)
		}

		(*i)[*accessor.Key] = channel
	default:
		return fmt.Errorf("component declared invalid output of type %T, expected one of these: %T, %T, %T", outputValue, new(chan<- T), new([]chan<- T), new(map[string]chan<- T))
	}

	return nil
}

func UpdateReceivers[T any](g *Graph, channel chan T, receivers2 ...string) error {
	for _, l := range receivers2 {
		split := strings.Split(l, ".")
		lName := split[0]
		lRawAccessor := split[1]
		g.Connections[l] = channel

		err := SetInPort(g.Components[lName], lRawAccessor, channel)
		if err != nil {
			return err
		}
	}

	return nil
}

func UpdateSenders[T any](g *Graph, channel chan T, senders2 ...string) error {
	for _, l := range senders2 {
		split := strings.Split(l, ".")
		lName := split[0]
		lRawAccessor := split[1]
		g.ChanCloseHandlersMap[lName] = NewChanCloseHandlerFunc(channel)
		g.Connections[l] = channel

		err := SetOutPort(g.Components[lName], lRawAccessor, channel)
		if err != nil {
			return err
		}
	}

	return nil
}

// Connect connects output of the first component to the input of the second component
func Connect[T any](g *Graph, c1name, c1outName, c2name, c2inName string) error {
	_, ok := g.Components[c1name]
	if !ok {
		return fmt.Errorf("graph does not contain component %s", c1name)
	}
	_, ok = g.Components[c2name]
	if !ok {
		return fmt.Errorf("graph does not contain component %s", c2name)
	}

	ch1, ok1 := g.Connections[fmt.Sprintf("%s.%s", c1name, c1outName)]
	ch2, ok2 := g.Connections[fmt.Sprintf("%s.%s", c2name, c2inName)]

	channel := make(chan T)
	if ok1 && ok2 {
		g.ChanOpenSenders[ch1] += g.ChanOpenSenders[ch2]
		delete(g.ChanOpenSenders, ch2)

		senders2 := g.ChanSenders[ch2]
		g.ChanSenders[ch1] = append(g.ChanSenders[ch1], senders2...)
		delete(g.ChanSenders, ch2)

		receivers2 := g.ChanReceivers[ch2]
		g.ChanReceivers[ch1] = append(g.ChanReceivers[ch1], receivers2...)
		delete(g.ChanReceivers, ch2)

		channel = ch1.(chan T)
		err := UpdateReceivers(g, channel, receivers2...)
		if err != nil {
			return err
		}

		return UpdateSenders(g, channel, senders2...)
	} else if ok1 {
		receiverName := fmt.Sprintf("%s.%s", c2name, c2inName)
		g.ChanReceivers[ch1] = append(g.ChanReceivers[ch1], receiverName)
		channel = ch1.(chan T)

		return UpdateReceivers(g, channel, receiverName)
	} else if ok2 {
		senderName := fmt.Sprintf("%s.%s", c1name, c1outName)
		g.ChanOpenSenders[ch2]++
		g.ChanSenders[ch2] = append(g.ChanSenders[ch2], senderName)
		channel = ch2.(chan T)

		return UpdateSenders(g, channel, senderName)
	}

	senderName := fmt.Sprintf("%s.%s", c1name, c1outName)
	g.ChanOpenSenders[channel] = 1
	g.ChanSenders[channel] = append(g.ChanSenders[channel], senderName)

	err := UpdateSenders(g, channel, senderName)
	if err != nil {
		return err
	}

	receiverName := fmt.Sprintf("%s.%s", c2name, c2inName)
	g.ChanReceivers[channel] = append(g.ChanReceivers[channel], receiverName)
	return UpdateReceivers(g, channel, receiverName)
}

func GetInputValue(c2 Component, c2inName1 string) (any, Accessor, error) {
	accessor := ParseAccessor(c2inName1)

	c2in, ok := c2.Inputs()[accessor.Field]
	if !ok {
		return nil, Accessor{}, fmt.Errorf("component has no input %s", accessor.Field)
	}

	if g, ok := c2.(*Graph); ok {
		split := strings.Split(c2in.(string), ".")
		return GetInputValue(g.Components[split[0]], split[1])
	}

	return c2in, accessor, nil
}

func GetOutputValue(c2 Component, c2inName1 string) (any, Accessor, error) {
	accessor := ParseAccessor(c2inName1)

	c2in, ok := c2.Outputs()[accessor.Field]
	if !ok {
		return nil, Accessor{}, fmt.Errorf("component has no output %s", accessor.Field)
	}

	if g, ok := c2.(*Graph); ok {
		split := strings.Split(c2in.(string), ".")
		return GetOutputValue(g.Components[split[0]], split[1])
	}

	return c2in, accessor, nil
}

type Accessor struct {
	Field string
	Index *int
	Key   *string
}

func ParseAccessor(accessor string) Accessor {
	var (
		c2inName string
		index    *int
		key      *string
	)

	split := strings.Split(accessor, "[")
	c2inName = split[0]
	if len(split) > 1 {
		c2inName = split[0]
		keyOrIdx := strings.TrimRight(split[1], "]")

		idx, err := strconv.Atoi(keyOrIdx)
		if err != nil {
			key = &keyOrIdx
		} else {
			index = &idx
		}
	}

	return Accessor{
		Field: c2inName,
		Index: index,
		Key:   key,
	}
}

func AddIIP[T any](g *Graph, componentName, inputName string, data T) error {
	_, ok := g.Components[componentName]
	if !ok {
		return fmt.Errorf("graph does not contain component %s", componentName)
	}

	channel := make(chan T)
	ch1, ok1 := g.Connections[fmt.Sprintf("%s.%s", componentName, inputName)]
	if ok1 {
		channel = ch1.(chan T)
	} else {
		err := UpdateReceivers(g, channel, fmt.Sprintf("%s.%s", componentName, inputName))
		if err != nil {
			return err
		}
	}

	g.ChanOpenSenders[channel]++

	g.InitialDataProviders = append(g.InitialDataProviders, func(g *Graph) {
		channel <- data

		closeHandler := NewChanCloseHandlerFunc[T](channel)
		closeHandler(g)
	})

	return nil
}
