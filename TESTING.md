# Testing

How to test everything this repo ships without touching Nelson's desktop. A test here
never opens a window on his screen, and "it would open a window" is never a reason to
leave something untested: it goes on a hidden desktop. A bump of fzt or fzt-terminal
reaches everything Go here, so it gets all three checks below, not only the part the PR
is about.

| What changed | Run |
| --- | --- |
| `frontend/fzt-files` | [fzt-files](#fzt-files), then its real terminal check |
| `frontend/cgo`, or a library bump | [The file dialog](#the-file-dialog) |
| `frontend/main.go`, `frontend/picker`, or a library bump | [The terminal picker](#the-terminal-picker) |

Work in a worktree under `D:\workspace\fzt-picker\<branch>`. This is a gaming PC, so keep
builds and tests to four cores. Never point a test at real files.

## fzt-files

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

### Real terminal check

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

## The hidden desktop

Windows can run a program on a second desktop that is never switched to. Nothing on it
reaches the screen or takes focus, and its windows still exist, take messages and paint.
`tools/hidden-desktop/hidden_launch.py <name> <command line>` starts a command on a new one
and waits for it; use a new name each run. Everything a process starts stays on its desktop,
so a tmux server started there keeps its panes there too. Use a test folder outside
`AppData`, such as `D:\workspace\fp-qa`, and delete it after.

## The file dialog

`dialog_test.py` loads the frontend DLL, calls `PickFile`, posts keys to its window and
screenshots it. It tests the installed build unless given another DLL as a third argument,
and ends itself after 25 seconds if the dialog sticks.

```sh
python=$(python -c "import sys;print(sys.executable)")
mkdir /d/workspace/fp-qa
for s in file folder inside cancel twice; do
  python tools/hidden-desktop/hidden_launch.py "fp-dialog-$s-$$" "\"$python\" $(cygpath -w "$PWD/tools/hidden-desktop/dialog_test.py") D:\\workspace\\fp-qa $s"
  cat /d/workspace/fp-qa/report-$s.json
done
```

| Scenario | Expect in its report |
| --- | --- |
| `file` | `picked` is `take-1.wav`'s full path, `finished` true |
| `folder` | `picked` is `sub` |
| `inside` | `picked` is `sub`, after going into it with Right and pressing Enter |
| `cancel` | `picked` is null |
| `twice` | `first` is the file and `second` is `sub`: the second dialog of a run on one thread |

Read `file-typed.png` in the same folder: it shows the list, the match and the highlight.

A global hook only reaches its own desktop, so this tests the DLL, not the COM hook. Running
the injector and `test-trigger.exe` (`cargo build -j 4`) on one hidden desktop should cover a
change under `windows/`, but that hasn't been tried yet.

## The terminal picker

`fzt-picker-frontend` calls `SetForegroundWindow` on its console, so start its throwaway
server from a hidden desktop to keep that call there. `caller.py` starts it as `fzt-automate`
does, with stdout captured. Give `respawn-pane` a plain argument list: a command line goes
through the pane's shell, which is pwsh and mangles the quoting.

```sh
python=$(python -c "import sys;print(sys.executable)")
tools=$(cygpath -w "$PWD/tools/hidden-desktop")
picker='C:\Program Files\at-home\fzt-picker\current\fzt-picker-frontend.exe'
mkdir -p /d/workspace/fp-qa/sub
python tools/hidden-desktop/hidden_launch.py "fp-term-$$" '"C:\Program Files\at-home\at-home-tools\current\tmux-throwaway.exe" start fpqa -- -s q -x 90 -y 26'
MSYS_NO_PATHCONV=1 tmux -L fpqa set -g remain-on-exit on
MSYS_NO_PATHCONV=1 tmux -L fpqa respawn-pane -k -t q "$python" "$tools\caller.py" 'D:\workspace\fp-qa\pick.json' "$picker" --folders-only --start-dir 'D:\workspace\fp-qa'
MSYS_NO_PATHCONV=1 tmux -L fpqa capture-pane -p -t q
MSYS_NO_PATHCONV=1 tmux -L fpqa send-keys -t q -l sub
MSYS_NO_PATHCONV=1 tmux -L fpqa send-keys -t q Enter
cat /d/workspace/fp-qa/pick.json
tmux -L fpqa kill-server
```

Give the picker a second or so to start, and to write `pick.json` after Enter. Expect
`stdout` to be the path and `exit` 0. Without `--folders-only`, pick a file the same way.
Escape goes up one folder at a time, and from the drive roots exits 130 with empty `stdout`.

## Updating a running window

`fzt-files` holds no state beyond the session, and a release installs into a new version
folder, so a running window keeps working on its old build. To bring one up to date, find
its pane with `tmux list-panes -a -F '#{pane_id} #{pane_current_command}'`, then
`tmux respawn-pane -k -t <pane>` it with the same command line. Nelson never has to
reopen it himself. It's safe unless a copy or move is in progress, which shows in the
panel: a window cut off mid-copy leaves a `.fzt-files-part` beside the destination.
