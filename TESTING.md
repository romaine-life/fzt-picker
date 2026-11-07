# Testing fzt-files

Work in a worktree under `D:\workspace\fzt-picker\<branch>`. This is a gaming PC, so keep
builds and tests to four cores. Never point a test at real files.

```sh
cd frontend
GOMAXPROCS=4 go vet ./fzt-files
GOMAXPROCS=4 go test -p 4 ./picker ./fzt-files
CGO_ENABLED=0 GOMAXPROCS=4 go build -p 4 -o dist/fzt-files.exe ./fzt-files
```

| Test file | Covers |
| --- | --- |
| `fzt-files/ops_test.go` | Never replacing a file, bad names, drives, a folder into itself, copies and their times, stopping a copy, moving between drives, links, what reaches the log |
| `fzt-files/flow_test.go` | The sentence in the title, the rename selection, Escape stepping back, a failure asking again, delete's confirmation, a direct command's exit and output, `build.yml` stamping the info pane |

`recycle` needs a real Recycle Bin, so it's checked by hand in the run below.

## Real terminal check

Run on a throwaway server, never the live `at` server. `FZT_FILES_DATA` keeps the run out
of the real log, which is copied to the NAS. Use a test folder outside `AppData`: the
picker hides hidden folders, so it can't walk down to one.

```sh
tmux-throwaway start ffqa -- -s q -x 90 -y 26
MSYS_NO_PATHCONV=1 tmux -L ffqa set -g remain-on-exit on
MSYS_NO_PATHCONV=1 tmux -L ffqa respawn-pane -k -t q -e 'FZT_FILES_DATA=<test folder>\data' '<worktree>\frontend\dist\fzt-files.exe --start-dir <test folder>'
MSYS_NO_PATHCONV=1 tmux -L ffqa capture-pane -p -t q
MSYS_NO_PATHCONV=1 tmux -L ffqa send-keys -t q -l 'rename file'
MSYS_NO_PATHCONV=1 tmux -L ffqa send-keys -t q Enter
```

Go through each command and check:

- The title builds the sentence, and Escape takes one word back from any folder.
- Rename opens with the name selected and the extension not. Typing replaces the selection.
- A taken name shows why in the panel and asks again, with what was typed gone.
- `delete file` on a fixed drive puts the file in the Recycle Bin. On a network or removable
  drive the row says it can't be undone.
- `fzt-files rename <path>` lands on the text box, prints the new path and exits 0; Escape
  exits 130.
- `info` shows the repo, version, install path and checkout.
- The window can shrink to 34 by 10 without breaking.

Finish with `tmux -L ffqa kill-server` and delete the test folder.

## Updating a running window

`fzt-files` holds no state beyond the session, and a release installs into a new version
folder, so a running window keeps working on its old build. To bring one up to date, find
its pane with `tmux list-panes -a -F '#{pane_id} #{pane_current_command}'`, then
`tmux respawn-pane -k -t <pane>` it with the same command line. Nelson never has to
reopen it himself. It's safe unless a copy or move is in progress, which shows in the
panel: a window cut off mid-copy leaves a `.fzt-files-part` beside the destination.
