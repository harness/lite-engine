// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

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
