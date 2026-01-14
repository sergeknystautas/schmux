## Overview

This directory has raw input from tmux capture to allow us to create workable test cases.

## Instructions

Figure out which terminal session you need:

tmux ls

Capture the session to a file from this directory:

tmux capture-pane -e -p -S -100 -t "session name" > claudeN.txt

# Conventions

We are naming based on the agent first, and then an incremental number.

The coding agent updating the tests will prepare the want.

# Updating expected output

If the extractor changes, update the `*.want.txt` files to match the new behavior.
