Leadlight
=========

A terminal UI for `Patchwork <https://github.com/getpatchwork/patchwork>`_
to review the status of patch series, delegate them, change their state,
and apply them with ``git am``.

Configuration
-------------

Leadlight reads configuration from gitconfig.  Run it from within the
git repository of the project you want to track.

If `git-pw <https://github.com/getpatchwork/git-pw>`_ is already
configured (``pw.server``, ``pw.project``), leadlight picks up those
settings automatically.

Otherwise, use the ``leadlight.*`` namespace::

  git config leadlight.server https://patchwork.example.com/api/1.3
  git config leadlight.project your-project

  # Optional: API token for write access (state/delegate changes)
  git config leadlight.token YOUR_TOKEN

  # Required for API 1.2 instances (which lack comment events)
  git config leadlight.mailarchive https://lists.example.com/pipermail/dev/

Starring patches
~~~~~~~~~~~~~~~~

Press ``*`` to star the selected patch, or the whole series when a
series row is selected.  Starred patches are marked with a ``★`` in
the first column, and ``!`` narrows the list to just them.  Stars are
stored locally, per repository, and are not sent to Patchwork.

Patches can also be starred automatically.  Each of these keys may be
repeated, and a patch matching any value is starred::

  # Star patches you are on the To or Cc line of.  Defaults to
  # user.email, so this usually needs no configuration.
  git config --add leadlight.star-email you@example.com

  # Star patches whose subject contains a keyword
  git config --add leadlight.star-keyword netdev

  # Star patches from a particular submitter (name or email)
  git config --add leadlight.star-submitter "Jane Doe"

  # Turn automatic starring off entirely
  git config leadlight.autostar false

Matching is case-insensitive and on substrings.  Starring or
unstarring a patch by hand always wins over the rules, so a star you
remove stays removed.  Editing the rules re-derives the automatic
stars on the next start, leaving the ones you set by hand alone.

Because ``To`` and ``Cc`` are only known once a patch's details have
been fetched, email stars on a fresh database appear gradually as the
background sync works through the list.  Keyword and submitter rules
apply as soon as a patch shows up.

Examples
~~~~~~~~

Open vSwitch::

  git config leadlight.server https://patchwork.ozlabs.org/api/1.2
  git config leadlight.project openvswitch
  git config leadlight.mailarchive https://mail.openvswitch.org/pipermail/ovs-dev/

OVN::

  git config leadlight.server https://patchwork.ozlabs.org/api/1.2
  git config leadlight.project ovn
  git config leadlight.mailarchive https://mail.openvswitch.org/pipermail/ovs-dev/

DPDK::

  git config leadlight.server https://patches.dpdk.org/api/1.3
  git config leadlight.project dpdk

Linux kernel netdev::

  git config leadlight.server https://patchwork.kernel.org/api/1.3
  git config leadlight.project netdevbpf

Building
--------

Requires Go 1.24 or later and a C compiler (gcc or clang) for the
SQLite driver.

::

  make build

To install into ``$GOBIN`` (default ``~/go/bin``)::

  make install

Then run ``leadlight`` from the git repository of the project you
want to track.

License
-------

Apache-2.0.  See `LICENSE <LICENSE>`_ and `AUTHORS <AUTHORS>`_.
