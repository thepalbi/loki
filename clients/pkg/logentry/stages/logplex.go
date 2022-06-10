package stages

type logplexStage struct {
}

func newLogplexStage() (Stage, error) {
	return &logplexStage{}, nil
}

func (l *logplexStage) Name() string {
	return StageTypeLogplex
}

func (l *logplexStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range in {
			err := l.processEntry(e)
			if err != nil {
				// dropping errored stuff
				continue
			}
			out <- e
		}
	}()
	return out
}

func (l *logplexStage) processEntry(e Entry) error {
	return nil
}
