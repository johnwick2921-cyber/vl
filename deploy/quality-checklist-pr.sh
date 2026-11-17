#!/usr/bin/env bash
# quality-checklist-pr.sh <body-file> <diff-file>
#
# Checks a pull request body against docs/QUALITY-CHECKLIST.md. The body must
# carry one bullet line per row, Q1 to Q14, and each line must say either
#   proof: <what proves it>        (at least 8 non-space characters)
#   n/a: <why it does not apply>   (at least 10 non-space characters)
# A tick or an empty checkbox on its own is not proof. Every Go test named on a
# row (TestSomething) must appear, as a whole name, on an ADDED line of the diff
# file, so a row cannot cite a test this PR never touched, only kept as
# context, or deleted.
#
# MENTOR TRADING CODE (owner order 2026-11-17). When the diff touches
# kernel/mentor/ or trader/mentor*, two rows get a machine check:
#   Q1  must be "proof:" and cite an October video by day and timestamp,
#       "D<n>.<m> @mm:ss" (the sources are listed in docs/QUALITY-CHECKLIST.md).
#   Q14 must be "proof:" and carry the money replay: the words "live" and
#       "branch" and at least two dollar amounts ("$1,871 live vs $1,733
#       branch"). Matching the video is not proof the change makes money:
#       release #72 matched the video and its new ISB rule would have cost
#       $425.50 on the 11-16 night the live version won.
#
# Exit 0 when every row passes, 1 otherwise, with one plain line per problem.
#
# The body is UNTRUSTED text written by whoever opened the PR. It is only ever
# read line by line as data: no eval, no command built from it, and every value
# taken from it is quoted. No network.
set -euo pipefail

ROWS=14
MIN_PROOF=8
MIN_NA=10

if [ "$#" -ne 2 ]; then
	echo "usage: quality-checklist-pr.sh <body-file> <diff-file>" >&2
	exit 1
fi
body_file=$1
diff_file=$2
if [ ! -r "$body_file" ]; then
	echo "quality checklist: body file not readable: $body_file"
	exit 1
fi
if [ ! -r "$diff_file" ]; then
	echo "quality checklist: diff file not readable: $diff_file"
	exit 1
fi

problems=()
declare -A seen=()
declare -A rowtext=()
declare -A cited=()

# A row line is a markdown bullet whose first word is Q<n>, optionally after a
# checkbox and optionally in bold: "- Q3 ...", "* [x] Q3 ...", "- **Q3** ...".
row_re='^[[:space:]]*[-*+][[:space:]]+(\[[ xX]\][[:space:]]*)?\**Q([0-9]+)([^0-9]|$)'
test_re='Test[A-Z][A-Za-z0-9_]*'

# count_meaningful prints how many characters of $1 are left once spaces,
# tick marks and checkboxes are taken out. "proof: [x] ✅" counts 0.
count_meaningful() {
	local s=$1
	s=${s//[[:space:]]/}
	s=${s//\[x\]/}
	s=${s//\[X\]/}
	s=${s//\[\]/}
	s=${s//✔️/}
	s=${s//☑️/}
	s=${s//✓/}
	s=${s//✔/}
	s=${s//✅/}
	s=${s//☑/}
	printf '%s' "${#s}"
}

in_comment=0
while IFS= read -r line || [ -n "$line" ]; do
	line=${line%$'\r'}

	# Drop HTML comments. They are invisible on the rendered PR page, so a row
	# hidden inside one would satisfy this check while no reviewer could see it.
	visible=""
	rest=$line
	while :; do
		if [ "$in_comment" -eq 1 ]; then
			if [[ $rest == *"-->"* ]]; then
				rest=${rest#*-->}
				in_comment=0
			else
				rest=""
				break
			fi
		else
			if [[ $rest == *"<!--"* ]]; then
				visible+=${rest%%<!--*}
				rest=${rest#*<!--}
				in_comment=1
			else
				visible+=$rest
				break
			fi
		fi
	done
	line=$visible

	if [[ ! $line =~ $row_re ]]; then
		continue
	fi
	n=$((10#${BASH_REMATCH[2]}))
	id="Q$n"
	if [ "$n" -lt 1 ] || [ "$n" -gt "$ROWS" ]; then
		problems+=("$id: there is no row $id in docs/QUALITY-CHECKLIST.md (rows are Q1 to Q$ROWS)")
		continue
	fi
	if [ -n "${seen[$id]:-}" ]; then
		problems+=("$id: the row appears more than once; keep one line per row")
		continue
	fi
	seen[$id]=1
	rowtext[$id]=$line

	lower=${line,,}
	ok=0
	if [[ $lower == *"proof:"* ]]; then
		proof=${lower#*proof:}
		if [ "$(count_meaningful "$proof")" -ge "$MIN_PROOF" ]; then
			ok=1
		fi
	fi
	if [ "$ok" -eq 0 ] && [[ $lower == *"n/a:"* ]]; then
		reason=${lower#*n/a:}
		if [ "$(count_meaningful "$reason")" -ge "$MIN_NA" ]; then
			ok=1
		fi
	fi
	if [ "$ok" -eq 0 ]; then
		if [[ $lower == *"n/a:"* ]]; then
			problems+=("$id: the n/a reason is too short; say in at least $MIN_NA characters why this row does not apply")
		elif [[ $lower == *"proof:"* ]]; then
			problems+=("$id: the proof is empty or too short; name the test, the frame or the log line ($MIN_PROOF+ characters, a tick is not proof)")
		else
			problems+=("$id: no proof; write \"proof: <test names / frame cite / pasted red line>\" or \"n/a: <reason>\" (a tick alone is not proof)")
		fi
		continue
	fi

	# Every Go test this row cites must be in the diff.
	while IFS= read -r name; do
		[ -n "$name" ] || continue
		cited[$name]="${cited[$name]:-}${cited[$name]:+ }$id"
	done < <(printf '%s\n' "$line" | grep -oE "$test_re" || true)
done < "$body_file"

for ((n = 1; n <= ROWS; n++)); do
	if [ -z "${seen[Q$n]:-}" ]; then
		problems+=("Q$n: the row is missing; add a line \"- Q$n ...: proof: ...\" or \"- Q$n ...: n/a: ...\"")
	fi
done

# Mentor trading code: Q1 needs a video cite, Q14 needs the money replay. A
# diff header is "diff --git a/<path> b/<path>"; either side counts, so a file
# moved out of kernel/mentor/ is still caught.
mentor_re='^diff --git (a/(kernel/mentor/|trader/mentor)[^ ]*|.* b/(kernel/mentor/|trader/mentor)[^ ]*)'
cite_re='D[1-5]\.[1-9][^@]{0,40}@[0-9]{1,2}:[0-9]{2}'
money_re='\$[0-9]'
if grep -qE "$mentor_re" "$diff_file"; then
	q1=${rowtext[Q1]:-}
	q1_lower=${q1,,}
	if [ -n "$q1" ] && { [[ $q1_lower != *"proof:"* ]] || ! grep -qE "$cite_re" <<<"$q1"; }; then
		problems+=("Q1: this PR changes mentor trading code (kernel/mentor/ or trader/mentor*); Q1 must be \"proof:\" with an October video cite \"D<n>.<m> @mm:ss\" from the sources in docs/QUALITY-CHECKLIST.md")
	fi
	q14=${rowtext[Q14]:-}
	q14_lower=${q14,,}
	if [ -n "$q14" ]; then
		dollars=$(grep -oE "$money_re" <<<"$q14" | wc -l || true)
		if [[ $q14_lower != *"proof:"* ]] || [[ $q14_lower != *"live"* ]] || [[ $q14_lower != *"branch"* ]] || [ "$dollars" -lt 2 ]; then
			problems+=("Q14: this PR changes mentor trading code; Q14 must be \"proof:\" with the money replay: the live version's \$ and this branch's \$ on the same recent sessions (the words \"live\" and \"branch\" and two dollar amounts)")
		fi
	fi
fi

# Only ADDED lines count: a test this PR deletes ("-" lines), or one that is
# only context around an edit (" " lines), is not something this PR proves.
# The "+++ b/path" file headers are not added lines.
added=$(grep -E '^\+' "$diff_file" | grep -vE '^\+\+\+ (b/|/dev/null)' || true)
for name in "${!cited[@]}"; do
	if ! grep -qwF -- "$name" <<<"$added"; then
		problems+=("${cited[$name]}: cites $name, but $name is not in this PR's diff; cite a test this PR adds or changes")
	fi
done

if [ "${#problems[@]}" -gt 0 ]; then
	printf '%s\n' "${problems[@]}" | sort -V
	echo "quality checklist: ${#problems[@]} problem(s). See docs/QUALITY-CHECKLIST.md."
	exit 1
fi
echo "quality checklist: all $ROWS rows have proof or a reason."
exit 0
