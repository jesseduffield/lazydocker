![buildah logo](https://cdn.rawgit.com/containers/buildah/main/logos/buildah-logo_large.png)

# Contributing to Buildah

We'd love to have you join the community! Below summarizes the processes
that we follow.

## Topics

* [Reporting Issues](#reporting-issues)
* [Working On Issues](#working-on-issues)
* [Submitting Pull Requests](#submitting-pull-requests)
* [Sign your PRs](#sign-your-prs)
* [Merge bot interaction](#merge-bot-interaction)
* [Communications](#communications)
* [Becoming a Maintainer](#becoming-a-maintainer)

## Reporting Issues

Before reporting an issue, check our backlog of
[open issues](https://github.com/containers/buildah/issues)
to see if someone else has already reported it. If so, feel free to add
your scenario, or additional information, to the discussion. Or simply
"subscribe" to it to be notified when it is updated.

If you find a new issue with the project we'd love to hear about it! The most
important aspect of a bug report is that it includes enough information for
us to reproduce it. So, please include as much detail as possible and try
to remove the extra stuff that doesn't really relate to the issue itself.
The easier it is for us to reproduce it, the faster it'll be fixed!

Please don't include any private/sensitive information in your issue!

## Working On Issues

Once you have decided to contribute to Buildah by working on an issue, check our
backlog of [open issues](https://github.com/containers/buildah/issues) looking
for any that do not have an "In Progress" label attached to it.  Often issues
will be assigned to someone, to be worked on at a later time.  If you have the
time to work on the issue now, add yourself as an assignee, and set the
"In Progress" label if you’re a member of the “Containers” GitHub organization.
If you can not set the label, just  add a quick comment in the issue asking that
the “In Progress” label be set and a member will do so for you.

## Submitting Pull Requests

No Pull Request (PR) is too small! Typos, additional comments in the code,
new testcases, bug fixes, new features, more documentation, ... it's all
welcome!

While bug fixes can first be identified via an "issue", that is not required.
It's ok to just open up a PR with the fix, but make sure you include the same
information you would have included in an issue - like how to reproduce it.

PRs for new features should include some background on what use cases the
new code is trying to address. When possible and when it makes sense, try to break-up
larger PRs into smaller ones - it's easier to review smaller
code changes. But only if those smaller ones make sense as stand-alone PRs.

Regardless of the type of PR, all PRs should include:
* well documented code changes
* additional testcases. Ideally, they should fail w/o your code change applied
* documentation changes

Squash your commits into logical pieces of work that might want to be reviewed
separate from the rest of the PRs. But, squashing down to just one commit is ok
too since in the end the entire PR will be reviewed anyway. When in doubt,
squash.

PRs that fix issues should include a reference like `Closes #XXXX` in the
commit message so that github will automatically close the referenced issue
when the PR is merged.

<!--
All PRs require at least two LGTMs (Looks Good To Me) from maintainers.
-->

### Sign your PRs

The sign-off is a line at the end of the explanation for the patch. Your
signature certifies that you wrote the patch or otherwise have the right to pass
it on as an open-source patch. The rules are simple: if you can certify
the below (from [developercertificate.org](http://developercertificate.org/)):

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
660 York Street, Suite 102,
San Francisco, CA 94110 USA

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Then you just add a line to every git commit message:

    Signed-off-by: Joe Smith <joe.smith@email.com>

Use your real name (sorry, no pseudonyms or anonymous contributions.)

If you set your `user.name` and `user.email` git configs, you can sign your
commit automatically with `git commit -s`.

## Merge bot interaction

Maintainers should never merge anything directly into upstream
branches.  Instead, interact with the [openshift-ci-robot](https://github.com/openshift-ci-robot/)
through PR comments as summarized [here](https://prow.ci.openshift.org/command-help?repo=containers%2Fbuildah).
This ensures all upstream
branches contain commits in a predictable order, and that every commit
has passed automated testing at some point in the past. A
[Maintainer portal](https://prow.ci.openshift.org/pr?query=is%3Apr%20state%3Aopen%20repo%3Acontainers%2Fbuildah)
is available, showing all PRs awaiting review and approval.

## Communications

For general questions or discussions, please use the
IRC channel `#podman` on `irc.libera.chat`. If you are unfamiliar with IRC you can start a web client at https://web.libera.chat/#podman.

Alternatively, [\[matrix\]](https://matrix.org) can be used to access the same channel via federation at https://matrix.to/#/#podman:chat.fedoraproject.org.

### For discussions around issues/bugs and features:

#### GitHub
You can also use GitHub
[issues](https://github.com/containers/buildah/issues)
and
[PRs](https://github.com/containers/buildah/pulls)
tracking system.

#### Buildah Mailing List


You can join the Buildah mailing list by sending an email to `buildah-join@lists.buildah.io` with the word `subscribe` in the subject.  You can also go to this [page](https://lists.podman.io/admin/lists/buildah.lists.buildah.io/), then scroll down to the bottom of the page and enter your email and optionally name, then click on the "Subscribe" button.

## Becoming a Maintainer

To become a maintainer you must first be nominated by an existing maintainer.
If a majority (>50%) of maintainers agree then the proposal is adopted and
you will be added to the list.

Removing a maintainer requires at least 75% of the remaining maintainers
approval, or if the person requests to be removed then it is automatic.
Normally, a maintainer will only be removed if they are considered to be
inactive for a long period of time or are viewed as disruptive to the community.

The current list of maintainers can be found in the
[MAINTAINERS](./MAINTAINERS.md) file.

