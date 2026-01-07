/*
 * Copying and distribution of this file, with or without modification,
 * are permitted in any medium without royalty provided the copyright
 * notice and this notice are preserved.  This file is offered as-is,
 * without any warranty.
 */

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include "gosequoia.h"

#if defined(GO_SEQUOIA_ENABLE_DLOPEN) && GO_SEQUOIA_ENABLE_DLOPEN

#include <assert.h>
#include <dlfcn.h>
#include <errno.h>
#include <stdlib.h>

/* If SEQUOIA_SONAME is defined, dlopen handle can be automatically
 * set; otherwise, the caller needs to call
 * go_sequoia_ensure_library with soname determined at run time.
 */
#ifdef SEQUOIA_SONAME

static void
ensure_library (void)
{
  if (go_sequoia_ensure_library (SEQUOIA_SONAME, RTLD_LAZY | RTLD_LOCAL) < 0)
    abort ();
}

#if defined(GO_SEQUOIA_ENABLE_PTHREAD) && GO_SEQUOIA_ENABLE_PTHREAD
#include <pthread.h>

static pthread_once_t dlopen_once = PTHREAD_ONCE_INIT;

#define ENSURE_LIBRARY pthread_once(&dlopen_once, ensure_library)

#else /* GO_SEQUOIA_ENABLE_PTHREAD */

#define ENSURE_LIBRARY do {	    \
    if (!go_sequoia_dlhandle) \
      ensure_library();		    \
  } while (0)

#endif /* !GO_SEQUOIA_ENABLE_PTHREAD */

#else /* SEQUOIA_SONAME */

#define ENSURE_LIBRARY do {} while (0)

#endif /* !SEQUOIA_SONAME */

static void *go_sequoia_dlhandle;

/* Define redirection symbols */
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wunused-macros"

#if (2 <= __GNUC__ || (4 <= __clang_major__))
#define FUNC(ret, name, args, cargs)			\
  static __typeof__(name)(*go_sequoia_sym_##name);
#else
#define FUNC(ret, name, args, cargs)		\
  static ret(*go_sequoia_sym_##name)args;
#endif
#define VOID_FUNC FUNC
#include "gosequoiafuncs.h"
#undef VOID_FUNC
#undef FUNC

#pragma GCC diagnostic pop

/* Define redirection wrapper functions */
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wunused-macros"

#define FUNC(ret, name, args, cargs)        \
ret go_##name args           \
{					    \
  ENSURE_LIBRARY;			    \
  assert (go_sequoia_sym_##name);	    \
  return go_sequoia_sym_##name cargs;	    \
}
#define VOID_FUNC(ret, name, args, cargs)   \
ret go_##name args           \
{					    \
  ENSURE_LIBRARY;			    \
  assert (go_sequoia_sym_##name);	    \
  go_sequoia_sym_##name cargs;		    \
}
#include "gosequoiafuncs.h"
#undef VOID_FUNC
#undef FUNC

#pragma GCC diagnostic pop

static int
ensure_symbol (const char *name, void **symp)
{
  if (!*symp)
    {
      void *sym = dlsym (go_sequoia_dlhandle, name);
      if (!sym)
	return -EINVAL;
      *symp = sym;
    }
  return 0;
}

int
go_sequoia_ensure_library (const char *soname, int flags)
{
  int err;

  if (!go_sequoia_dlhandle)
    {
      go_sequoia_dlhandle = dlopen (soname, flags);
      if (!go_sequoia_dlhandle)
	return -EINVAL;
    }

#define ENSURE_SYMBOL(name)					\
  ensure_symbol(#name, (void **)&go_sequoia_sym_##name)

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wunused-macros"

#define FUNC(ret, name, args, cargs)		\
  err = ENSURE_SYMBOL(name);			\
  if (err < 0)					\
    {						\
      dlclose (go_sequoia_dlhandle);	\
      go_sequoia_dlhandle = NULL;		\
      return err;				\
    }
#define VOID_FUNC FUNC
#include "gosequoiafuncs.h"
#undef VOID_FUNC
#undef FUNC

#pragma GCC diagnostic pop

#undef ENSURE_SYMBOL
  return 0;
}

void
go_sequoia_unload_library (void)
{
  if (go_sequoia_dlhandle)
    {
      dlclose (go_sequoia_dlhandle);
      go_sequoia_dlhandle = NULL;
    }

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wunused-macros"

#define FUNC(ret, name, args, cargs)		\
  go_sequoia_sym_##name = NULL;
#define VOID_FUNC FUNC
#include "gosequoiafuncs.h"
#undef VOID_FUNC
#undef FUNC

#pragma GCC diagnostic pop
}

unsigned
go_sequoia_is_usable (void)
{
  return go_sequoia_dlhandle != NULL;
}

#else /* GO_SEQUOIA_ENABLE_DLOPEN */

int
go_sequoia_ensure_library (const char *soname, int flags)
{
  (void) soname;
  (void) flags;
  return 0;
}

void
go_sequoia_unload_library (void)
{
}

unsigned
go_sequoia_is_usable (void)
{
  /* The library is linked at build time, thus always usable */
  return 1;
}

#endif /* !GO_SEQUOIA_ENABLE_DLOPEN */
