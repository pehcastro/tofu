package host

import (
	"context"
	"sync"
	"time"

	"tofu/internal/session"
	"tofu/internal/turn"
)

type answeredQuestion struct {
	QuestionAnswer
	by string
}

type questions struct {
	mu      sync.Mutex
	waiting map[string]chan answeredQuestion
}

func (q *questions) wait(id string) chan answeredQuestion {
	reply := make(chan answeredQuestion, 1)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.waiting[id] = reply
	return reply
}

func (q *questions) answer(id string, given answeredQuestion) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	reply, open := q.waiting[id]
	if open {
		delete(q.waiting, id)
		reply <- given
	}
	return open
}

func (q *questions) forget(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.waiting, id)
}

func (h *Host) questionsBlock() (blocks, decided bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.asking == AskingAsk, h.asking != ""
}

func (h *Host) AnswerQuestion(id string, answer QuestionAnswer, by string) bool {
	return h.questions.answer(id, answeredQuestion{answer, by})
}

func askForm(emit func(Event), book *questions) turn.PersonForm {
	return func(ctx context.Context, asked []turn.PersonQuestion, wait time.Duration) ([]turn.PersonReply, error) {
		id := session.NewEventID()
		reply := book.wait(id)
		defer book.forget(id)
		emit(Event{Kind: EventAwaitPerson, ID: id, Tool: turn.AskPersonToolName, Text: asked[0].Question, Questions: asked, Wait: wait})
		select {
		case answered := <-reply:
			emit(Event{Kind: EventResumed, ID: id, Text: string(answered.Outcome), Detail: answered.by})
			switch answered.Outcome {
			case QuestionSubmitted:
				return answered.Answers, nil
			case QuestionCancelled:
				return nil, turn.QuestionDismissed{}
			case QuestionUndelivered:
				return nil, turn.QuestionUndelivered{Why: answered.by + " cannot answer questions"}
			}
			panic("host: unknown question outcome " + string(answered.Outcome))
		case <-ctx.Done():
			emit(Event{Kind: EventResumed, ID: id, Text: string(QuestionCancelled), Detail: "tofu"})
			return nil, ctx.Err()
		}
	}
}
