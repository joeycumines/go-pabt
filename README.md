# go-pabt

An implementation of the planning algorithm described by chapter 7 of
https://www.researchgate.net/publication/319463746_Behavior_Trees_in_Robotics_and_AI_An_Introduction, in golang, built
using https://github.com/joeycumines/go-behaviortree

## Overview

The PA-BT planning algorithm uses a so-called "reachability graph", combined
with "continual hill-climbing", to incrementally find and execute a suitable
plan, to achieve a given goal, in disjunctive normal form (DNF). It is well
suited for composing disparate sets of "actions" which have interconnected
"effects" or "conditions", such as narrower, domain-specific algorithms.
In the "pick and place" example, PA-BT is used to unify path finding and object
manipulation.

This algorithm is relatively simple, does not support "optimal" planning
natively, though it may be guided towards a more optimal plan, through the use
of conditions. Conceptually, this is "constraint programming" as opposed to
"constrained optimization".

## Use cases

- Compose simpler "action templates" into more complex plans
- Perform tasks in changing environments
- Interoperability with any other behavior tree or compatible implementation

## Examples

### tcell-pick-and-place

![tcell-pick-and-place demo 1](https://imgur.com/W0NfhSY.gif "A demonstration of the example")
