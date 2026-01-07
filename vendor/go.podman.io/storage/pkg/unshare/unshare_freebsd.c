#if !defined(UNSHARE_NO_CODE_AT_ALL) && defined(__FreeBSD__)


#include <sys/types.h>
#include <sys/ioctl.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

static int _containers_unshare_parse_envint(const char *envname) {
	char *p, *q;
	long l;

	p = getenv(envname);
	if (p == NULL) {
		return -1;
	}
	q = NULL;
	l = strtol(p, &q, 10);
	if ((q == NULL) || (*q != '\0')) {
		fprintf(stderr, "Error parsing \"%s\"=\"%s\"!\n", envname, p);
		_exit(1);
	}
	unsetenv(envname);
	return l;
}

void _containers_unshare(void)
{
	int pidfd, continuefd, n, pgrp, sid, ctty;
	char buf[2048];

	pidfd = _containers_unshare_parse_envint("_Containers-pid-pipe");
	if (pidfd != -1) {
		snprintf(buf, sizeof(buf), "%llu", (unsigned long long) getpid());
		size_t size = write(pidfd, buf, strlen(buf));
		if (size != strlen(buf)) {
			fprintf(stderr, "Error writing PID to pipe on fd %d: %m\n", pidfd);
			_exit(1);
		}
		close(pidfd);
	}
	continuefd = _containers_unshare_parse_envint("_Containers-continue-pipe");
	if (continuefd != -1) {
		n = read(continuefd, buf, sizeof(buf));
		if (n > 0) {
			fprintf(stderr, "Error: %.*s\n", n, buf);
			_exit(1);
		}
		close(continuefd);
	}
	sid = _containers_unshare_parse_envint("_Containers-setsid");
	if (sid == 1) {
		if (setsid() == -1) {
			fprintf(stderr, "Error during setsid: %m\n");
			_exit(1);
		}
	}
	pgrp = _containers_unshare_parse_envint("_Containers-setpgrp");
	if (pgrp == 1) {
                if (setpgrp(0, 0) == -1) {
			fprintf(stderr, "Error during setpgrp: %m\n");
			_exit(1);
		}
	}
	ctty = _containers_unshare_parse_envint("_Containers-ctty");
	if (ctty != -1) {
		if (ioctl(ctty, TIOCSCTTY, 0) == -1) {
			fprintf(stderr, "Error while setting controlling terminal to %d: %m\n", ctty);
			_exit(1);
		}
	}
}

#endif
