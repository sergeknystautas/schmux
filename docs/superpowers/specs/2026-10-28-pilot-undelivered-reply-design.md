# Reply to the pilot when its guidance cannot be delivered

## Intent

When the pilot sends guidance to an agent that is not available, schmux drops
the guidance and tells the pilot nothing. The user's example: they kill the
commit agent to reset the flight a few steps, the pilot keeps sending
guidance to `commit`, and the guidance goes nowhere with no word to the pilot.

The user's words: "we should tell the pilot that this agent is not available
anymore. Please give a response to the pilot." This is a routing failure
being reported, not a decision error being returned for the pilot to answer.

## Behavior

When schmux cannot deliver the pilot's guidance because the agent it
addressed is not available, schmux sends the pilot one reply:

```
Your guidance to <role> was not delivered: <reason>.
```

`<role>` is the role the pilot addressed (`planner`, `executor`, `reviewer`,
`commit`). The reply covers four cases:

| When                      | Condition                                                     | `<reason>`                                                         |
| ------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------------ |
| Decision (`pilot.go:391`) | the role is a working role with no started session            | `<role> session has not been started`                              |
| Decision (`pilot.go:398`) | the role's session is not running                             | `<role> session is not running`                                    |
| Delivery (`pilot.go:538`) | the recipient session no longer holds the role it was sent to | `<role> session <session id> is no longer assigned to this flight` |
| Delivery (`pilot.go:545`) | the recipient session is not running                          | `<role> session is not running`                                    |

Nothing else changes:

- The decision-time error stays on the pilot's turn exactly as today, and the
  delivery-time failure stays on the `pilot_message` as `failed` exactly as
  today. The flight story shows them as it does now.
- The reply is plain text. It carries no list of agents, no decision contract,
  and no request for a decision.
- Schmux does not resend, retry, or hold the guidance.
- Every other failure still sends nothing to the pilot: the decision errors
  (malformed JSON, unknown fields, a missing `to`, a `to` that is not a
  working role) and the other delivery failures (workspace mismatch, a
  sender that is no longer the assigned pilot, a phase the pilot no longer
  supervises, a chat runtime or send error).

The reply is a flight message of a new kind, `undelivered`, from schmux to
the pilot, with the pilot turn that produced the guidance as its source turn.
It is delivered the way launch notices are: recorded `pending` and sent by
the monitor's delivery loop.

## Non-goals

- Telling the pilot which agents are available.
- Any change to how the pilot addresses agents or to which agents it may
  address.
- Any dashboard change. The flight story's default message branch
  (`assets/dashboard/src/lib/flightStory.ts:413`) already shows nothing for a
  delivered pilot-bound message and an error line for a failed one.

## Left to the builder

- The helper that records the reply, and how the delivery-time case finds the
  role of a session that is no longer assigned (the assignment whose
  `prior_session_ids` contains it).

## Acceptance criteria

1. **Killed commit agent, at decision time.** A `completing` flight whose
   `commit` session is assigned but not running; the pilot decides
   `{"to":"commit","guidance":"Retry the commit."}`. The pilot's conversation
   receives `Your guidance to commit was not delivered: commit session is not running.`
   with no `[Pilot]` prefix. No `pilot_message` is recorded, and the pilot
   turn is `failed` with an error containing `commit session is not running`.
2. **Never-started agent, at decision time.** An `executing` flight with no
   `commit` assignment; the pilot decides `{"to":"commit","guidance":"…"}`. The
   pilot receives `Your guidance to commit was not delivered: commit session has not been started.`
   The pilot turn is `failed` with the same error it records today (it names
   the roles the flight has started).
3. **Agent stopped before delivery.** A `pilot_message` to the executor is
   recorded `pending`, the executor session then stops running, and delivery
   runs. The message is `failed` with `executor session is not running`, and
   the pilot receives `Your guidance to executor was not delivered: executor session is not running.`
4. **Agent replaced before delivery.** A `pilot_message` to the reviewer is
   recorded `pending`, the `reviewer` assignment then moves to a new session,
   and delivery runs. The message is `failed`, and the pilot receives
   `Your guidance to reviewer was not delivered: reviewer session <old session id> is no longer assigned to this flight.`
5. **Other decision errors stay silent.** A missing `to`, a `to` of `foo`,
   and malformed JSON each record a failed pilot turn and add no message to
   the flight (`TestDecisionErrorIsRecordedAndSendsNothing` and a case for
   `"to":"foo"`).
6. **The reply survives a restart.** A flight snapshot whose monitor history
   holds an `undelivered` message loads without quarantine and round-trips
   the message unchanged.
7. **Docs match the code.** `docs/flights.md` (the pilot decision paragraph
   and the "Schmux never answers the pilot back" decision) and `docs/api.md`
   (the decision-error paragraph and the list of message kinds) describe the
   reply and the `undelivered` kind. The message-kind comments at
   `internal/flights/model.go:451` and `internal/api/contracts/flights.go:251`
   list `undelivered`, and the stale comments at `internal/flights/pilot.go:224-231`
   and `pilot.go:526` describe the reply.
8. **Checks pass.** `./test.sh` passes, `./badcode.sh` passes, and
   `go run ./cmd/gen-types` leaves `assets/dashboard/src/lib/types.generated.ts`
   unchanged (the generator does not copy field comments).

Review artifact for the user: `review/pilot-undelivered-reply.html`, a
self-contained page that shows, for each of the four cases in criteria 1-4,
the guidance the pilot sent, the exact reply text the pilot's conversation
received, and the test name that proves it, with the `./test.sh` output.
