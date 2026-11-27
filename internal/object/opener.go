package object

import (
	"fmt"
	"strings"
	"text/template"
)

// OpenerData is what the opener template of a conversation sees.
type OpenerData struct {
	Title string
	// Missing are the fields the state still lacks, in schema order.
	Missing []Field
	Filled  int
	Total   int
}

// NewOpenerData builds the opener data of a state.
func NewOpenerData(title string, fields []Field, state map[string]any) OpenerData {
	missing := Missing(fields, state)
	set := map[string]bool{}
	for _, m := range missing {
		set[m] = true
	}
	d := OpenerData{Title: title}
	for _, f := range fields {
		if !f.Required && f.MinItems == 0 {
			continue
		}
		d.Total++
		if set[f.Name] {
			d.Missing = append(d.Missing, f)
		} else {
			d.Filled++
		}
	}
	return d
}

// ParseOpener checks an opener template.
func ParseOpener(name, text string) (*template.Template, error) {
	return template.New(name).Option("missingkey=zero").Funcs(template.FuncMap{"lower": strings.ToLower}).Parse(text)
}

// Opener renders the first message of a conversation: without a model, from what the state lacks.
// An empty template gives a plain list.
func Opener(text string, d OpenerData) (string, error) {
	if strings.TrimSpace(text) == "" {
		if len(d.Missing) == 0 {
			return fmt.Sprintf("%s: nothing is missing. Tell me what to add or change.", d.Title), nil
		}
		names := make([]string, len(d.Missing))
		for i, f := range d.Missing {
			names[i] = f.Name
			if f.Title != "" {
				names[i] = f.Title
			}
		}
		next := d.Missing[0].Question
		if next == "" {
			next = names[0] + "?"
		}
		return fmt.Sprintf("%s: %d of %d filled, missing: %s. %s", d.Title, d.Filled, d.Total,
			strings.Join(names, ", "), next), nil
	}
	t, err := ParseOpener("opener", text)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, d); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}
