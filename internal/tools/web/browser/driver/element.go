package browser

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"tofu/internal/konst"
)

type Accessible struct {
	Role, Name, Value string
	Attributes        map[string]*string
}

func (a Accessible) String() string {
	if a.Role == "" {
		return ""
	}
	return a.Role + " " + strconv.Quote(a.Name)
}

func (d *Driver) Focused() (Accessible, error) {
	var tree struct {
		Nodes []axNode `json:"nodes"`
	}
	answer, err := d.one(time.Now().Add(konst.BrowserActTimeoutMillis*time.Millisecond), false, cdpCall{Method: "Accessibility.getFullAXTree"})
	if err == nil {
		err = answer.into(&tree)
	}
	var focused Accessible
	for _, node := range tree.Nodes {
		if !node.Ignored && node.property("focused") == "true" && node.Role.text() != "RootWebArea" {
			focused = Accessible{Role: node.Role.text(), Name: node.Name.text()}
		}
	}
	return focused, err
}

func (n axNode) property(name string) string {
	for _, property := range n.Properties {
		if property.Name == name {
			return property.Value.text()
		}
	}
	return ""
}

const computedStyle = "function(names) { const style = getComputedStyle(this); return Object.fromEntries(names.map(name => [name, style.getPropertyValue(name)])); }"

func (d *Driver) Styles(ref string, names []string) (map[string]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	deadline := time.Now().Add(konst.BrowserActTimeoutMillis * time.Millisecond)
	object, err := d.resolve(deadline, ref)
	read := map[string]string{}
	if err == nil {
		err = d.value(deadline, false, callOn(object, computedStyle, true, names), &read)
	}
	return read, err
}

func (d *Driver) Read(ref string, attributes []string) (Accessible, error) {
	deadline := time.Now().Add(konst.BrowserActTimeoutMillis * time.Millisecond)
	var described struct {
		Node struct {
			Attributes []string `json:"attributes"`
		} `json:"node"`
	}
	var tree struct {
		Nodes []axNode `json:"nodes"`
	}
	_, err := d.onNode(deadline, ref, func(backend int) error {
		answers, err := d.cdp(deadline, false, byBackend("DOM.describeNode", backend),
			cdpCall{Method: "Accessibility.getPartialAXTree", Params: map[string]any{"backendNodeId": backend, "fetchRelatives": false}})
		if err != nil {
			return err
		}
		return errors.Join(answers[0].into(&described), answers[1].into(&tree))
	})
	if err == nil && len(tree.Nodes) == 0 {
		err = fmt.Errorf("tab %d has no accessible node for the ref %s", d.Tab, ref)
	}
	if err != nil {
		return Accessible{}, err
	}
	node := tree.Nodes[0]
	read := Accessible{Role: node.Role.text(), Name: node.Name.text(), Value: node.Value.text(), Attributes: map[string]*string{}}
	for _, name := range attributes {
		read.Attributes[name] = nil
	}
	pairs := described.Node.Attributes
	for i := 0; i+1 < len(pairs); i += 2 {
		if _, asked := read.Attributes[pairs[i]]; asked {
			read.Attributes[pairs[i]] = &pairs[i+1]
		}
	}
	return read, nil
}
