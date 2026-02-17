# Wishlist

This is the stuff that we hope to be adding to Schmux in the near future.
We might come up with new ideas or we might implement these ideas
and find out they were bad. Think of it as an exploratory wishlist
rather than a series of future milestones.

## Mobile Access

- Watch what's going on in the software factory whie on the go
- UX is trickier on mobile phones
- Push notifications when there are significant status updates

## Floor Manager

- Run the software factory thru the mediation of an agent that
  is aware of everything that is going on in the factory.
- We describe the software we want, the floor manager helps us
  keep the factory going.
- It should be able to monitors progress, reassigns work,
  handles failures autonomously, interface with agents and unblock
  them or notify the user for help when needed.

## Clean Onboarding

- First CLI run guides you through setup without friction
- Web dashboard doesn't overwhelm new users with every option upfront
- Automated testing makes sure we don't break the onboarding experience
  since developers onboard very rarely
- Smooth out `dev.sh` first-run experiencef for new schmux developers

## Analytics

- how many people are using schmux?
- how are they using it?
- what walls are they running into?
- tricky for open source projects to do this, but seems morally
  sound if we give the telemetry aggregates back to the community
  and set quick TTL for raw data.

## Multiplayer Schmux

### Phase 1 - Hosted Schmux

- Run schmux in the cloud where multiple users can manage the software
  factory together.
- Useful for teams that want to work together but want to share
  token costs.
- Will require cloud hosting and that's fine as generally this is
  something teams are capable of provision and pay of.

### Phase 2 - Schmux Federation

- Another type of multiplayer software factory but this one is
  useful for developers who want to contribute to the same effort
  (say open source project) but can't or don't want to join
  a token cost center.
- Bonus points if this can be operated p2p without needing to
  host any specialized software in the cloud as it's less clear
  who should operate and be responsible for the costs of this
  shared infrastructure.
