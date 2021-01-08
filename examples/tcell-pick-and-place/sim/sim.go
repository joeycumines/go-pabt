// Copyright 2021 Joseph Cumines
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build example

package sim

import (
	"context"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	baseWidth       = 80.0
	baseHeight      = 24.0
	hudWidth        = 24.0
	hudHeight       = baseHeight
	spaceWidth      = baseWidth - hudWidth
	spaceHeight     = baseHeight
	runeExtra       = '#'
	stepDistance    = 6.0 / tickPerSecond
	tickPerSecond   = 30.0
	defaultInterval = time.Second / tickPerSecond
	pickupDistance  = 1.42
	infoText        = `pick and place

1 on goal = win

MANUAL CONTROLS
arrows = move
space = stop/start
wasd = turn
numbers = pick
'r' = place`
)

type (
	Simulation interface {
		Run(ctx context.Context) error

		State() *State

		// Move will attempt to move a Sprite to a position on the screen, going directly there (no attempt at
		// pathfinding), note that it must be called with a key from the Sprites map
		Move(ctx context.Context, sprite Sprite, x, y float64) error

		Grasp(ctx context.Context, sprite Sprite, target Sprite) error

		Release(ctx context.Context, sprite Sprite, target Sprite) error
	}

	Config struct {
		Screen   tcell.Screen
		Interval time.Duration // tick interval
	}

	Space struct {
		Room  bool
		Floor bool
	}

	service struct {
		*state
		config            Config
		model             *model
		runMutex          sync.Mutex
		running           int32
		actions           bool
		tickChan          <-chan time.Time
		keyChan           <-chan *tcell.EventKey
		resizeChan        <-chan *tcell.EventResize
		externalLogicChan chan externalLogic
	}

	update struct {
		*model

		// WARNING for clarity make sure these fields are distinct from model's

		Actions []func()
		Redraw  bool
		Lock    bool
	}

	model struct {
		State    *state
		Time     time.Time
		Interval time.Duration
		Dirty    bool
		// window width and height
		Width, Height int32
		// approximation of a robot doing picking and placing
		Actors []*actorModel
		// things able to be picked up
		Cubes []*cubeModel
		// where the actor needs to take the target cube
		Goals []*goalModel
		// executed each update once per tick until returns true
		ExternalLogic []externalLogic
	}

	spriteModel struct {
		X, Y          float64       // location (top left)
		Width, Height int32         // size
		DX, DY        float64       // direction + magnitude
		Stop          bool          // skips movement but keeps direction / magnitude
		Images        []spriteImage // image stack see also spriteImage
		Image         []rune        // last image runes
		Shape         Shape         // shape must be set if the sprite is visible and must be added to the space
		Owner         interface{}   // Owner is what this sprite is for
		Space         Space         // flags indicating what it should collide with
	}

	spriteImage interface {
		// Runes are the actual sprite, 0 is transparent, fills top lhs -> rhs, within width / height
		Runes() []rune
		// Expired indicates the image can be removed from the stack
		Expired() bool
	}

	staticImage []rune

	imageExpiry struct {
		spriteImage
		expired func() bool
	}

	actorModel struct {
		Sprite   *spriteModel
		Criteria Criteria
		Keyboard bool
		HeldItem Sprite
	}

	cubeModel struct {
		Sprite *spriteModel
	}

	goalModel struct {
		Sprite *spriteModel
	}

	externalLogic func(ctx context.Context, u *update) bool
)

var (
	_ Simulation = (*service)(nil)
)

var (
	actorSpace = Space{
		Room: true,
	}
	cubeSpace = Space{
		Room: true,
	}
	goalSpace = Space{
		Floor: true,
	}
)

var (
	NewSpriteShape = newRectangle
)

func New(config Config) (Simulation, error) {
	if config.Screen == nil {
		return nil, fmt.Errorf(`nil screen`)
	}
	if config.Interval == 0 {
		config.Interval = defaultInterval
	}
	if config.Interval <= 0 {
		return nil, fmt.Errorf(`invalid interval: %s`, config.Interval)
	}
	svc := &service{
		state: &state{
			sprites: make(map[*spriteModel]*spriteModel),
			actors:  make(map[*actorModel]*actorModel),
			cubes:   make(map[*cubeModel]*cubeModel),
			goals:   make(map[*goalModel]*goalModel),
		},
		config:            config,
		actions:           true,
		externalLogicChan: make(chan externalLogic),
	}
	svc.view(svc.init(config))
	return svc, nil
}

func (s *service) init(config Config) (u update) {
	u.model = &model{
		State:    s.state,
		Time:     time.Now(),
		Interval: config.Interval,
	}
	u.Width, u.Height = sizeInt32(s.config.Screen.Size())

	// built as part of this func
	var planConfig PlanConfig
	u.Actions = append(u.Actions, func() { u.State.plan = planConfig })

	if sprite, err := u.createSprite(30-hudWidth, 10, 3, 2, []rune(`0|00|0`)); err != nil {
		panic(err)
	} else if actor, err := u.createActor(sprite); err != nil {
		panic(err)
	} else {
		planConfig.Actors = append(planConfig.Actors, u.State.new(actor.Sprite, actor).(Actor))

		actor.Keyboard = true

		// build actor criteria (initialising other relevant sprites)
		{
			var (
				actorGoal Goal
				actorCube Cube
			)

			if sprite, err := u.createSprite(77-hudWidth, 9, 3, 5, []rune(`!G!!O!!A!!L!!!!`)); err != nil {
				panic(err)
			} else if goal, err := u.createGoal(sprite); err != nil {
				panic(err)
			} else {
				actorGoal = u.State.new(goal.Sprite, goal).(Goal)
			}

			if sprite, err := u.createSprite(60-hudWidth, 8, 1, 1, []rune(`1`)); err != nil {
				panic(err)
			} else if cube, err := u.createCube(sprite); err != nil {
				panic(err)
			} else {
				actorCube = u.State.new(cube.Sprite, cube).(Cube)
			}

			actor.Criteria[CriteriaKey{Cube: actorCube, Goal: actorGoal}] = CriteriaValue{}
		}
	}

	if sprite, err := u.createSprite(77-hudWidth, 8, 1, 1, []rune(`2`)); err != nil {
		panic(err)
	} else if _, err := u.createCube(sprite); err != nil {
		panic(err)
	}
	if sprite, err := u.createSprite(75-hudWidth, 9, 1, 1, []rune(`3`)); err != nil {
		panic(err)
	} else if _, err := u.createCube(sprite); err != nil {
		panic(err)
	}
	if sprite, err := u.createSprite(74-hudWidth, 11, 1, 1, []rune(`4`)); err != nil {
		panic(err)
	} else if _, err := u.createCube(sprite); err != nil {
		panic(err)
	}
	if sprite, err := u.createSprite(75-hudWidth, 13, 1, 1, []rune(`5`)); err != nil {
		panic(err)
	} else if _, err := u.createCube(sprite); err != nil {
		panic(err)
	}
	if sprite, err := u.createSprite(77-hudWidth, 14, 1, 1, []rune(`6`)); err != nil {
		panic(err)
	} else if _, err := u.createCube(sprite); err != nil {
		panic(err)
	}

	u.Redraw = true
	u.Dirty = false
	u.Lock = true
	return
}
func (s *service) view(u update) {
	s.model = u.model
	if s.actions {
		func() {
			if u.Lock {
				s.state.mu.Lock()
				defer s.state.mu.Unlock()
			}
			for _, action := range u.Actions {
				action()
			}
		}()
	}
	if u.Redraw {
		s.config.Screen.Clear()

		// draw border (fills everything outside the scene with runeExtra)
		{

			var (
				extraWidth  = u.Width > baseWidth
				extraHeight = u.Height > baseHeight
			)
			if extraWidth {
				for y := int32(0); y < u.Height && y < baseHeight; y++ {
					for x := int32(baseWidth); x < u.Width; x++ {
						s.config.Screen.SetContent(int(x), int(y), runeExtra, nil, tcell.StyleDefault)
					}
				}
			}
			if extraHeight {
				for x := int32(0); x < u.Width && x < baseWidth; x++ {
					for y := int32(baseHeight); y < u.Height; y++ {
						s.config.Screen.SetContent(int(x), int(y), runeExtra, nil, tcell.StyleDefault)
					}
				}
			}
			if extraWidth && extraHeight {
				for x := int32(baseWidth); x < u.Width; x++ {
					for y := int32(baseHeight); y < u.Height; y++ {
						s.config.Screen.SetContent(int(x), int(y), runeExtra, nil, tcell.StyleDefault)
					}
				}
			}
		}

		// draw sprites
		u.sprites(false, func(sprite *spriteModel) bool {
			if sprite.visible() {
				s.drawSprite(sprite)
			}
			return true
		})

		// terrible hud
		{
			for y := int32(0); y < hudHeight; y++ {
				s.config.Screen.SetContent(hudWidth-1, int(y), '|', nil, tcell.StyleDefault)
			}
			for y, line := range strings.Split(fmt.Sprintf(
				"%s\n\n%s",
				infoText,
				u.statusPane(),
			), "\n") {
				if y >= hudHeight {
					break
				}
				for x, v := range []rune(line) {
					if x >= hudWidth-1 {
						break
					}
					s.config.Screen.SetContent(x, y, v, nil, tcell.StyleDefault)
				}
			}
		}

		s.config.Screen.Show()
	}
}
func (s *service) drawSprite(sprite *spriteModel) { sprite.draw(s.config.Screen.SetContent) }
func (s *service) update(ctx context.Context) (u update) {
	u.model = s.model
	select {
	case <-ctx.Done():
	case u.Time = <-s.tickChan:
		u.ExternalLogic = u.externalLogic(ctx)
		u.move()
		if u.Dirty {
			u.Redraw = true
			u.Dirty = false
		}
	case event := <-s.keyChan:
		switch event.Key() {
		case tcell.KeyCtrlC:
			u.Actions = append(u.Actions, func() { atomic.StoreInt32(&s.running, 0) })
		case tcell.KeyUp:
			u.controlActorsKeyboard(true, 0, -stepDistance)
		case tcell.KeyDown:
			u.controlActorsKeyboard(true, 0, stepDistance)
		case tcell.KeyLeft:
			u.controlActorsKeyboard(true, -stepDistance, 0)
		case tcell.KeyRight:
			u.controlActorsKeyboard(true, stepDistance, 0)
		case tcell.KeyRune:
			switch event.Rune() {
			case 'w':
				u.controlActorsKeyboard(false, 0, -stepDistance)
			case 's':
				u.controlActorsKeyboard(false, 0, stepDistance)
			case 'a':
				u.controlActorsKeyboard(false, -stepDistance, 0)
			case 'd':
				u.controlActorsKeyboard(false, stepDistance, 0)
			case ' ':
				u.toggleStopActorsKeyboard()
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
				for _, cube := range s.model.Cubes {
					if sprite := cube.sprite(); sprite.visible() && sprite.Images[0].Runes()[0] == event.Rune() {
						if u.graspItemActorKeyboard(sprite) {
							continue
						}
					}
				}
			case 'r':
				u.releaseHeldItemActorsKeyboard()
			}
		}
	case event := <-s.resizeChan:
		if w, h := sizeInt32(event.Size()); w != u.Width || h != u.Height {
			u.Width, u.Height = w, h
			u.Dirty = true
		}
	case fn := <-s.externalLogicChan:
		u.ExternalLogic = append(u.ExternalLogic, fn)
	}
	if u.Redraw {
		u.sprites(false, func(sprite *spriteModel) bool {
			if sprite.visible() {
				sprite.Image = sprite.image().Runes()
			}
			return true
		})
	}
	return
}
func (s *service) Run(ctx context.Context) error {
	s.runMutex.Lock()
	atomic.StoreInt32(&s.running, 1)
	defer func() {
		atomic.StoreInt32(&s.running, 0)
		s.runMutex.Unlock()
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.startTicker(ctx)
	s.startEventLoop(ctx)
	for s.running == 1 {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.view(s.update(ctx))
	}
	return nil
}
func (s *service) startTicker(ctx context.Context) {
	ticker := time.NewTicker(s.model.Interval)
	go func() {
		<-ctx.Done()
		ticker.Stop()
	}()
	s.tickChan = ticker.C
}
func (s *service) startEventLoop(ctx context.Context) {
	var (
		keyChan    = make(chan *tcell.EventKey)
		resizeChan = make(chan *tcell.EventResize)
	)
	go eventLoop(
		ctx,
		s.config.Screen.PollEvent,
		keyChan,
		resizeChan,
	)
	s.keyChan = keyChan
	s.resizeChan = resizeChan
}
func eventLoop(
	ctx context.Context,
	poll func() tcell.Event,
	keyChan chan<- *tcell.EventKey,
	resizeChan chan<- *tcell.EventResize,
) {
	var (
		event tcell.Event
	)
	for {
		if ctx.Err() != nil {
			return
		}
		event = poll()
		if event == nil {
			return
		}
		switch event := event.(type) {
		case *tcell.EventKey:
			select {
			case <-ctx.Done():
				return
			case keyChan <- event:
			}
		case *tcell.EventResize:
			select {
			case <-ctx.Done():
				return
			case resizeChan <- event:
			}
		}
	}
}
func (s *service) externalLogic(ctx context.Context, fn func(ctx context.Context, u *update) bool) (err error) {
	err = ctx.Err()
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		runCtx context.Context
		ready  = make(chan struct{})
		done   = make(chan struct{})
		call   = func(u *update) bool {
			if ctx.Err() != nil {
				return true
			}
			if !fn(ctx, u) {
				return false
			}
			close(done)
			return true
		}
		logic externalLogic = func(c context.Context, u *update) bool {
			select {
			case <-ready:
			default:
				runCtx = c
				close(ready)
			}
			return call(u)
		}
	)

	// wait for runCtx
	func() {
		ctx, cancel := context.WithTimeout(ctx, time.Second*5+s.config.Interval)
		defer cancel()
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case s.externalLogicChan <- logic:
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case <-ready:
		}
	}()
	if err != nil {
		return
	}

	select {
	case <-ctx.Done():
		err = ctx.Err()
	case <-runCtx.Done():
		err = runCtx.Err()
	case <-done:
	}

	return
}
func (s *service) Move(ctx context.Context, sprite Sprite, x, y float64) error {
	const (
		delta = 0.1
	)
	var (
		equal  = func(x2, y2 float64) bool { return math.Abs(x-x2) <= delta && math.Abs(y-y2) <= delta }
		err    error
		shadow *spriteModel
	)
	if e := s.externalLogic(ctx, func(ctx context.Context, u *update) bool {
		sprite := sprite.sprite()
		if !u.spriteExists(sprite) {
			err = fmt.Errorf(`sprite not found`)
			return true
		}
		if x < 0 || x > spaceWidth-float64(sprite.Width) || y < 0 || y > spaceHeight-float64(sprite.Height) {
			err = fmt.Errorf(`target position invalid: %v, %v`, x, y)
			return true
		}
		if !sprite.visible() {
			err = fmt.Errorf(`sprite not visible`)
			return true
		}
		if equal(sprite.X, sprite.Y) {
			sprite.Stop = true
			return true
		}
		var interrupted bool
		if shadow == nil {
			d := calcDistance(sprite.X, sprite.Y, x, y)
			sprite.DX, sprite.DY = (x-sprite.X)/d*stepDistance, (y-sprite.Y)/d*stepDistance
			sprite.Stop = false
			shadow = sprite.clone()
			shadow.Space = Space{}
		} else if sprite.X != shadow.X ||
			sprite.Y != shadow.Y ||
			sprite.Width != shadow.Width ||
			sprite.Height != shadow.Height ||
			sprite.DX != shadow.DX ||
			sprite.DY != shadow.DY ||
			sprite.Stop != shadow.Stop {
			interrupted = true
		}
		if !interrupted {
			if _, modified := shadow.move(u.collides); !modified && !equal(shadow.X, shadow.Y) {
				interrupted = true
			}
		}
		if interrupted {
			err = fmt.Errorf("sprite movement interrupted")
			return true
		}
		return false
	}); err == nil {
		return e
	}
	return err
}
func (s *service) Grasp(ctx context.Context, sprite Sprite, target Sprite) error {
	return s.actionActorCube(ctx, sprite, target, (*update).graspItem)
}
func (s *service) Release(ctx context.Context, sprite Sprite, target Sprite) error {
	return s.actionActorCube(ctx, sprite, target, (*update).releaseItem)
}
func (s *service) actionActorCube(ctx context.Context, sprite Sprite, target Sprite, action func(*update, *actorModel, *spriteModel) bool) error {
	var (
		sm  = sprite.sprite()
		tm  = target.sprite()
		err error
	)
	if e := s.externalLogic(ctx, func(ctx context.Context, u *update) bool {
		{
			search := map[*spriteModel]struct{}{sm: {}, tm: {}}
			u.sprites(false, func(sprite *spriteModel) bool {
				delete(search, sprite)
				return len(search) != 0
			})
			if len(search) != 0 {
				err = fmt.Errorf("sprite or target not found")
				return true
			}
		}

		// sm and tm are valid sprite models (that we own), we can now inspect them freely

		actor, ok := sm.Owner.(*actorModel)
		if !ok {
			err = fmt.Errorf("unexpected sprite type: %T (%T)", sprite, sm.Owner)
			return true
		}

		if _, ok := tm.Owner.(*cubeModel); !ok {
			err = fmt.Errorf("unexpected target type: %T (%T)", target, tm.Owner)
			return true
		}

		if !action(u, actor, tm) {
			err = fmt.Errorf("action failed")
			return true
		}

		return true
	}); err == nil {
		return e
	}
	return err
}

func (u *update) createSprite(x, y float64, width, height int32, runes []rune) (*spriteModel, error) {
	vx, vy := RoundPosition(x, y)
	if err := validateSprite(vx, vy, width, height, runes); err != nil {
		return nil, err
	}
	sprite := &spriteModel{
		X:      x,
		Y:      y,
		Width:  width,
		Height: height,
		Images: []spriteImage{
			append(staticImage(nil), runes...),
		},
	}
	sprite.Shape = sprite.shapeAt(vx, vy)
	sprite.Image = sprite.image().Runes()
	u.updateSprite(sprite)
	return sprite, nil
}
func (u *update) updateSprite(sprite *spriteModel) {
	u.Lock = true
	u.Actions = append(u.Actions, func() { u.State.sprites[sprite] = sprite.clone() })
}
func (u *update) initSprite(sprite *spriteModel, owner interface{}, space Space) error {
	if sprite.Owner != nil || sprite.Space != (Space{}) {
		panic(sprite)
	}
	sprite.Space = space
	if u.collides(sprite) {
		sprite.Space = Space{}
		return fmt.Errorf(`invalid coordinates: collides with other sprite(s)`)
	}
	sprite.Owner = owner
	u.updateSprite(sprite)
	u.Dirty = true
	return nil
}
func (u *update) reposition(sprite *spriteModel, x, y float64) error {
	vx, vy := RoundPosition(x, y)
	if err := validateSprite(vx, vy, sprite.Width, sprite.Height, sprite.Image); err != nil {
		return err
	}
	{
		old := sprite.Shape
		sprite.Shape = sprite.shapeAt(vx, vy)
		if u.collides(sprite) {
			sprite.Shape = old
			return fmt.Errorf(`invalid coordinates: collides with other sprite(s)`)
		}
	}
	u.updateSprite(sprite)
	u.Dirty = true
	return nil
}
func (u *update) control(sprite *spriteModel, force bool, dx, dy float64) bool {
	forceStop := force && dx == 0 && dy == 0
	if sprite.visible() && (sprite.DX != dx || sprite.DY != dy || (force && sprite.Stop != forceStop)) {
		sprite.DX, sprite.DY = dx, dy
		if force {
			sprite.Stop = forceStop
		}
		u.updateSprite(sprite)
		u.Dirty = true
		return true
	}
	return false
}
func (u *update) toggleStop(sprite *spriteModel) bool {
	if sprite.visible() {
		sprite.Stop = !sprite.Stop
		u.updateSprite(sprite)
		u.Dirty = true
		return true
	}
	return false
}
func (u *update) setStop(sprite *spriteModel, stop bool) bool {
	if sprite.Stop != stop {
		return u.toggleStop(sprite)
	}
	return false
}
func (u *update) createActor(sprite *spriteModel) (*actorModel, error) {
	actor := &actorModel{
		Sprite:   sprite,
		Criteria: make(Criteria),
	}
	if err := u.initSprite(sprite, actor, actorSpace); err != nil {
		return nil, err
	}
	u.Actors = append(u.Actors, actor)
	u.updateActor(actor)
	return actor, nil
}
func (u *update) updateActor(actor *actorModel) {
	u.Lock = true
	u.Actions = append(u.Actions, func() { u.State.actors[actor] = actor.clone() })
}
func (u *update) createGoal(sprite *spriteModel) (*goalModel, error) {
	goal := &goalModel{
		Sprite: sprite,
	}
	if err := u.initSprite(sprite, goal, goalSpace); err != nil {
		return nil, err
	}
	u.Goals = append(u.Goals, goal)
	u.updateGoal(goal)
	return goal, nil
}
func (u *update) updateGoal(goal *goalModel) {
	u.Lock = true
	u.Actions = append(u.Actions, func() { u.State.goals[goal] = goal.clone() })
}
func (u *update) createCube(sprite *spriteModel) (*cubeModel, error) {
	cube := &cubeModel{
		Sprite: sprite,
	}
	if err := u.initSprite(sprite, cube, cubeSpace); err != nil {
		return nil, err
	}
	u.Cubes = append(u.Cubes, cube)
	u.updateCube(cube)
	return cube, nil
}
func (u *update) updateCube(cube *cubeModel) {
	u.Lock = true
	u.Actions = append(u.Actions, func() { u.State.cubes[cube] = cube.clone() })
}
func (u *update) move() {
	u.sprites(false, func(sprite *spriteModel) bool {
		if sprite.visible() {
			moved, modified := sprite.move(u.collides)
			if modified {
				u.updateSprite(sprite)
			}
			if moved {
				u.Dirty = true
			}
		}
		return true
	})
}
func (u *update) externalLogic(ctx context.Context) (remaining []externalLogic) {
	for _, fn := range u.ExternalLogic {
		if !fn(ctx, u) {
			remaining = append(remaining, fn)
		}
	}
	return
}
func (u *update) controlActors(force bool, dx, dy float64, filter func(actor *actorModel) bool) {
	for _, actor := range u.Actors {
		if filter(actor) {
			u.control(actor.sprite(), force, dx, dy)
		}
	}
}
func (u *update) toggleStopActors(filter func(actor *actorModel) bool) {
	for _, actor := range u.Actors {
		if filter(actor) {
			u.toggleStop(actor.sprite())
		}
	}
}
func (u *update) graspItemActor(sprite *spriteModel, filter func(actor *actorModel) bool) bool {
	for _, actor := range u.Actors {
		if filter(actor) && u.graspItem(actor, sprite) {
			return true
		}
	}
	return false
}
func (u *update) releaseHeldItemActors(filter func(actor *actorModel) bool) {
	for _, actor := range u.Actors {
		if filter(actor) && actor.HeldItem != nil {
			u.releaseItem(actor, actor.HeldItem.sprite())
		}
	}
}
func (u *update) controlActorsKeyboard(force bool, dx, dy float64) {
	u.controlActors(force, dx, dy, actorFilterKeyboard)
}
func (u *update) toggleStopActorsKeyboard() { u.toggleStopActors(actorFilterKeyboard) }
func (u *update) graspItemActorKeyboard(sprite *spriteModel) bool {
	return u.graspItemActor(sprite, actorFilterKeyboard)
}
func (u *update) releaseHeldItemActorsKeyboard() {
	u.releaseHeldItemActors(actorFilterKeyboard)
}
func (u *update) graspItem(actor *actorModel, sprite *spriteModel) bool {
	if actor.HeldItem != nil || !sprite.visible() || len(sprite.Images) != 1 {
		return false
	}

	actorSprite := actor.sprite()
	if !actorSprite.visible() || len(actorSprite.Images) != 1 {
		return false
	}

	if actorSprite.distance(sprite) > pickupDistance {
		return false
	}

	actorImage := append(staticImage(nil), actorSprite.Images[0].Runes()...)
	actorImage[int(actorSprite.Width)/2] = sprite.Images[0].Runes()[0]

	{
		cs := u.State.new(sprite, sprite.Owner)
		actor.HeldItem = cs
		actorSprite.Images = append(actorSprite.Images, imageExpiry{
			spriteImage: actorImage,
			expired:     func() bool { return actor.HeldItem != cs },
		})
	}
	sprite.Shape = nil
	u.updateActor(actor)
	u.updateSprite(actorSprite)
	u.updateSprite(sprite)
	u.Dirty = true

	return true
}
func (u *update) releaseItem(actor *actorModel, sprite *spriteModel) bool {
	if actor.HeldItem != u.State.new(sprite, sprite.Owner) {
		return false
	}

	actorSprite := actor.sprite()
	if !actorSprite.visible() {
		return false
	}

	if !sprite.visible() {
		x, y := actorSprite.Shape.Position()
		x, y = actorHeldItemReleasePosition(x, y, actorSprite.Width, sprite.Width, sprite.Height)
		if err := u.reposition(sprite, float64(x), float64(y)); err != nil {
			return false
		}
	}

	actor.HeldItem = nil

	u.updateActor(actor)
	u.updateSprite(actorSprite)
	u.Dirty = true

	return true
}

func (m *model) spriteExists(sprite *spriteModel) (exists bool) {
	if sprite != nil {
		m.sprites(false, func(o *spriteModel) bool {
			if o == sprite {
				exists = true
				return false
			}
			return true
		})
	}
	return
}
func (m *model) sprites(downward bool, fn func(sprite *spriteModel) bool) {
	if downward {
		panic(`downward unimplemented`)
	}
	for _, v := range m.Goals {
		if !callSpriteFn(v, fn) {
			return
		}
	}
	for _, v := range m.Cubes {
		if !callSpriteFn(v, fn) {
			return
		}
	}
	for _, v := range m.Actors {
		if !callSpriteFn(v, fn) {
			return
		}
	}
}
func (m *model) collisions(space Space, shape Shape, fn func(sprite *spriteModel) bool) {
	// TODO should probably resolve collisions downward
	m.sprites(false, func(sprite *spriteModel) bool {
		if sprite.collides(space, shape) {
			return fn(sprite)
		}
		return true
	})
}
func (m *model) collides(sprite *spriteModel) (collides bool) {
	m.collisions(sprite.Space, sprite.Shape, func(o *spriteModel) bool {
		if o == sprite {
			return true
		}
		collides = true
		return false
	})
	return
}
func (m *model) statusPane() (b []byte) {
	b = make([]byte, 0, hudWidth*hudHeight) // including newlines but less border
	b = append(b, "ACTOR STATUS\n"...)
	for i, actor := range m.Actors {
		name := fmt.Sprintf("%d", i)
		if sprite := actor.sprite(); sprite != nil {
			x, y := sprite.Shape.Position()
			b = append(b, fmt.Sprintf("%s.pos = %d, %d\n", name, x, y)...)
			b = append(b, fmt.Sprintf("%s.vel = %s\n", name, summarizeVelocity(sprite.DX, sprite.DY))...)
			b = append(b, fmt.Sprintf("%s.stop = %v\n", name, sprite.Stop)...)
			b = append(b, fmt.Sprintf("%s.hand = %s\n", name, func() string {
				if actor.HeldItem != nil {
					if v := actor.HeldItem.sprite(); v != nil && len(v.Image) != 0 && v.Height == 1 {
						return string(v.Image)
					}
					return `unknown`
				}
				return `none`
			}())...)
		}
	}
	return
}

func (m *spriteModel) collides(space Space, shape Shape) bool {
	return m.visible() &&
		collides(m.Space, m.Shape, space, shape)
}
func (m *spriteModel) clone() *spriteModel {
	var r spriteModel
	if m != nil {
		r.X, r.Y = m.X, m.Y
		r.Width, r.Height = m.Width, m.Height
		r.DX, r.DY = m.DX, m.DY
		r.Stop = m.Stop
		// images aren't safe (for use in the state) but whatever
		r.Images = append([]spriteImage(nil), m.Images...)
		r.Image = append([]rune(nil), m.Image...)
		if m.Shape != nil {
			r.Shape = m.Shape.Clone()
		}
		r.Owner = m.Owner
		r.Space = m.Space
	}
	return &r
}
func (m *spriteModel) move(collides func(*spriteModel) bool) (moved, modified bool) {
	if m.Stop {
		return
	}
	if m.DX == 0 && m.DY == 0 {
		return
	}

	{
		x, y := m.X, m.Y
		defer func() { modified = m.X != x || m.Y != y }()
	}

	m.X += m.DX
	m.Y += m.DY

	if m.X < 0 {
		m.X = 0
	} else if max := spaceWidth - float64(m.Width); m.X > max {
		m.X = max
	}

	if m.Y < 0 {
		m.Y = 0
	} else if max := spaceHeight - float64(m.Height); m.Y > max {
		m.Y = max
	}

	var (
		nx, ny = RoundPosition(m.X, m.Y)
		ox, oy = m.Shape.Position()
	)
	if nx == ox && ny == oy {
		return
	}

	m.Shape.SetPosition(nx, ny)
	if collides(m) {
		m.Shape.SetPosition(ox, oy)
		m.X = float64(ox)
		m.Y = float64(oy)
		return
	}

	moved = true
	return
}
func (m *spriteModel) draw(draw func(x int, y int, mainc rune, combc []rune, style tcell.Style)) {
	x, y := m.Shape.Position()
	if err := validateSprite(x, y, m.Width, m.Height, m.Image); err != nil {
		panic(err)
	}
	var i int
	for h := int32(0); h < m.Height && i < len(m.Image); h++ {
		for w := int32(0); w < m.Width && i < len(m.Image); w++ {
			v := m.Image[i]
			i++
			if v != 0 {
				draw(int(x+w+hudWidth), int(y+h), v, nil, tcell.StyleDefault)
			}
		}
	}
}
func (m *spriteModel) sprite() *spriteModel            { return m }
func (m *spriteModel) visible() bool                   { return m != nil && m.Shape != nil }
func (m *spriteModel) distance(o *spriteModel) float64 { return m.Shape.Distance(o.Shape) }
func (m *spriteModel) image() (v spriteImage) {
	if m != nil {
		for l := len(m.Images); l != 0; l = len(m.Images) {
			v = m.Images[l-1]
			if !v.Expired() {
				break
			}
			m.Images[l-1] = nil
			m.Images = m.Images[:l-1]
		}
	}
	return
}
func (m *spriteModel) shapeAt(vx, vy int32) Shape { return NewSpriteShape(vx, vy, m.Width, m.Height) }

func (m *actorModel) clone() *actorModel {
	var r actorModel
	if m != nil {
		r.Sprite = m.Sprite
		r.Criteria = make(Criteria, len(m.Criteria))
		for k, v := range m.Criteria {
			r.Criteria[k] = v
		}
		r.Keyboard = m.Keyboard
		r.HeldItem = m.HeldItem
	}
	return &r
}
func (m *actorModel) sprite() *spriteModel {
	if m != nil {
		return m.Sprite
	}
	return nil
}

func (m *cubeModel) clone() *cubeModel {
	var r cubeModel
	if m != nil {
		r.Sprite = m.Sprite
	}
	return &r
}
func (m *cubeModel) sprite() *spriteModel {
	if m != nil {
		return m.Sprite
	}
	return nil
}

func (m *goalModel) clone() *goalModel {
	var r goalModel
	if m != nil {
		r.Sprite = m.Sprite
	}
	return &r
}
func (m *goalModel) sprite() *spriteModel {
	if m != nil {
		return m.Sprite
	}
	return nil
}

func (x staticImage) Runes() []rune { return x }
func (x staticImage) Expired() bool { return false }

func (x imageExpiry) Expired() bool { return x.expired() }

func (x Space) Collides(o Space) bool {
	switch {
	case x.Room && o.Room:
	case x.Floor && o.Floor:
	default:
		return false
	}
	return true
}

func callSpriteFn(v interface{ sprite() *spriteModel }, fn func(sprite *spriteModel) bool) bool {
	if v := v.sprite(); v != nil {
		return fn(v)
	}
	return true
}

func actorFilterKeyboard(actor *actorModel) bool { return actor.Keyboard }

func validateSprite(x, y, width, height int32, runes []rune) error {
	if err := validateSpriteSpace(spaceWidth, spaceHeight, x, y, width, height); err != nil {
		return err
	}
	if l := width*height - int32(len(runes)); l > 0 {
		return fmt.Errorf(`invalid runes: excess of %d cells`, l)
	}
	return nil
}

func validateSpriteSpace(sw, sh, x, y, w, h int32) error {
	if w <= 0 {
		return fmt.Errorf(`invalid width: %d`, w)
	}
	if h <= 0 {
		return fmt.Errorf(`invalid height: %d`, h)
	}
	if x < 0 || x+w-1 >= sw {
		return fmt.Errorf(`invalid x/width: %d/%d`, x, w)
	}
	if y < 0 || y+h-1 >= sh {
		return fmt.Errorf(`invalid y/height: %d/%d`, y, h)
	}
	return nil
}

func summarizeVelocity(dx, dy float64) []byte {
	dx *= tickPerSecond
	dy *= tickPerSecond
	switch {
	case dx == 0 && dy == 0:
		return []byte(`none`)
	case dx == 0 && dy > 0:
		return []byte(fmt.Sprintf(`down %.1f/s`, dy))
	case dx == 0 && dy < 0:
		return []byte(fmt.Sprintf(`up %.1f/s`, -dy))
	case dx > 0 && dy == 0:
		return []byte(fmt.Sprintf(`right %.1f/s`, dx))
	case dx < 0 && dy == 0:
		return []byte(fmt.Sprintf(`left %.1f/s`, -dx))
	default:
		return []byte(fmt.Sprintf(`%.2f, %.2f`, dx, dy))
	}
}

func actorHeldItemReleasePosition(x, y, w, rw, rh int32) (rx, ry int32) {
	rx, ry = x+(w/2)-(rw/2), y-rh
	return
}

func collides(space1 Space, shape1 Shape, space2 Space, shape2 Shape) bool {
	return shape1 != nil &&
		shape2 != nil &&
		space1.Collides(space2) &&
		shape1.Collides(shape2)
}
