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
| `windows/` | [The hook](#the-hook), after `cargo test -j 4 -p fzt-picker-hook` |
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
of the real log, which is copied to the NAS. Use a test folder of your own, such as
`D:\workspace\fp-qa`.

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
- `open file` runs a file's default action: a `.cmd` that writes a file beside itself proves
  it without an app window. A type with no app starts `OpenWith.exe`, so run that one on
  [the hidden desktop](#the-hidden-desktop) and end the `OpenWith.exe` it left.
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
so a tmux server started there keeps its panes there too. Use a test folder of your own,
such as `D:\workspace\fp-qa`, and delete it after.

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

This tests the DLL alone. The hook decides whether it runs at all, so after a release run
[the hook](#the-hook) too.

## The hook

`hook_test.py` starts an injector and `test-trigger.exe` on a hidden desktop. A global hook
reaches only its own desktop, so this injector can't touch Nelson's apps or his running
injector, and only the one it started is stopped. `test-trigger` opens the system file
dialog, the hook swaps in fzt's, opened in the user folder, and the script goes up to the
drive roots with Left, walks down by name and reports what came back.

```sh
cd windows && cargo build -j 4 --release && cd ..
python=$(python -c "import sys;print(sys.executable)")
trigger=$(cygpath -w "$PWD/windows/target/release/test-trigger.exe")
mkdir /d/workspace/fp-qa
for s in file folder cancel; do
  python tools/hidden-desktop/hidden_launch.py "fp-hook-$s-$$" "\"$python\" $(cygpath -w "$PWD/tools/hidden-desktop/hook_test.py") D:\\workspace\\fp-qa $s \"$trigger\""
  cat /d/workspace/fp-qa/hook-$s.json
done
```

`file` and `folder` should end with `selected file:` and the path, and `cancel` with
`cancelled`. With `dialog shown` false, the hook gave up before drawing anything: its reason
is the last line of `%TEMP%\picker.log`. That is how FP-5 showed up: every hooked File > Open
was cancelling, because the hook looked for the dialog DLL in a folder releases no longer use.

### A release under a running app

`swap_test.py` checks that a running app moves to a new release's hook without restarting.
It lays out `v1`, `v2` and a `current` junction as home-sync does, starts the injector
through `current`, and runs `test-trigger --repeat`, which opens a dialog every five seconds
and makes windows in between as an app does. After the first dialog it swaps `current` to
`v2` and restarts the injector. The hook logs the folder it looked in, so `swap.json` lists
which release answered each dialog: it should be `v1`, then `v2`, `v2`.

```sh
cd windows && cargo build -j 4 --release && cd ..
python=$(python -c "import sys;print(sys.executable)")
build=$(cygpath -w "$PWD/windows/target/release")
mkdir /d/workspace/fp-qa
python tools/hidden-desktop/hidden_launch.py "fp-swap-$$" "\"$python\" $(cygpath -w "$PWD/tools/hidden-desktop/swap_test.py") D:\\workspace\\fp-qa \"$build\" \"$build\\test-trigger.exe\""
cat /d/workspace/fp-qa/swap.json
```

An error goes to `swap-error.txt` in the same folder. Before FP-6 the injector loaded the hook
through `current`, every release's copy had that one name, and the answer was the first copy
an app ever loaded, for as long as the app ran.

### On the live PC

`python tools/hook_versions.py` reads which release's hook each running process holds, and
changes nothing. After a release, every process should move to the new version as soon as
its windows next do anything.

This tests the installed injector, hook and dialog DLL. To test a local hook, add the folder
holding its `fzt-picker.exe` as a fifth argument. The dialog DLL has to be beside the hook,
and only CI can build it (it needs the runner's gcc), so locally the hook can only prove that
it looked in its own folder: `picker.log` names that folder.

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

A start folder inside a hidden one (make one with `attrib +h`) opens there, and going up
shows that hidden folder and no other. A start folder that isn't there opens at the last
folder found with `no folder <path>` in the title, which goes at the next key.

## Previewing a change for Nelson

A change to what he sees is shown to him before it ships, in his own window: run the
test build on the throwaway server from the isolated check above, then put it in the
bottom half of his pane with `tmux-throwaway show <server>` (its `-t` names a session
on that server, not his pane). Before telling him what it shows, read what he
will see: the shown pane is alive (`tmux display -p -t <shown pane> '#{pane_dead}'`
prints `0`) and `tmux -L <server> capture-pane -p` shows the case you're describing.
The turn his answer settles it, end the server with `tmux-throwaway end <server>` and
close the shown pane if it stays (AHT-114). Updating his window below is the same:
read it back before saying it's updated.

## Updating a running window

`fzt-files` holds no state beyond the session, and a release installs into a new version
folder, so a running window keeps working on its old build. To bring one up to date, find
its pane with `tmux list-panes -a -F '#{pane_id} #{pane_current_command}'`, then
`tmux respawn-pane -k -t <pane>` it with the same command line. Nelson never has to
reopen it himself. It's safe unless a copy or move is in progress, which shows in the
panel: a window cut off mid-copy leaves a `.fzt-files-part` beside the destination.
