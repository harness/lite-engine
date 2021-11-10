package errors

type BadRequestError struct {
	Msg string // description of error
}

func (e *BadRequestError) Error() string { return e.Msg }

type NotFoundError struct {
	Msg string // description of error
}

func (e *NotFoundError) Error() string { return e.Msg }

type InternalServerError struct {
	Msg string // description of error
}

func (e *InternalServerError) Error() string { return e.Msg }
