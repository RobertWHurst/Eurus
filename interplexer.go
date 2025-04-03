package eurus

import "github.com/RobertWHurst/velaros"

type interplexerConnection struct {
	service *Service
}

var _ velaros.InterplexerConnection = &interplexerConnection{}

func (i *interplexerConnection) AnnounceSocketOpen(interplexerID string, socketID string) error {
	return nil
}

func (i *interplexerConnection) BindSocketOpenAnnounce(handler func(interplexerID string, socketID string)) error {
	return nil
}

func (i *interplexerConnection) UnbindSocketOpenAnnounce() error {
	return nil
}

func (i *interplexerConnection) AnnounceSocketClose(interplexerID string, socketID string) error {
	return nil
}

func (i *interplexerConnection) BindSocketCloseAnnounce(handler func(interplexerID string, socketID string)) error {
	return nil
}

func (i *interplexerConnection) UnbindSocketCloseAnnounce() error {
	return nil
}

func (i *interplexerConnection) Dispatch(interplexerID string, socketID string, message []byte) error {
	return i.service.Transport.OutboundSocketMessage(socketID, message)
}

func (i *interplexerConnection) BindDispatch(interplexerID string, handler func(socketID string, message []byte) bool) error {
	return nil
}

func (i *interplexerConnection) UnbindDispatch(interplexerID string) error {
	return nil
}
