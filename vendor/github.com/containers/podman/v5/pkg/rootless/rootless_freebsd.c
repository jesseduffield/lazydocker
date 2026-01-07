#include <dirent.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <sys/select.h>

static int open_files_max_fd;
static fd_set *open_files_set;

int
is_fd_inherited(int fd)
{
  if (open_files_set == NULL || fd > open_files_max_fd || fd < 0)
    return 0;

  return FD_ISSET(fd % FD_SETSIZE, &(open_files_set[fd / FD_SETSIZE])) ? 1 : 0;
}

static void __attribute__((constructor)) init()
{
  /* Store how many FDs were open before the Go runtime kicked in.  */
  DIR* d = opendir ("/dev/fd");
  if (d)
    {
      struct dirent *ent;
      size_t size = 0;

      for (ent = readdir (d); ent; ent = readdir (d))
        {
          int fd;

          if (ent->d_name[0] == '.')
            continue;

          fd = atoi (ent->d_name);
          if (fd == dirfd (d)) {
            continue;
          }

          if (fd >= size * FD_SETSIZE)
            {
              int i;
              size_t new_size;

              new_size = (fd / FD_SETSIZE) + 1;
              open_files_set = realloc (open_files_set, new_size * sizeof (fd_set));
              if (open_files_set == NULL)
                _exit (EXIT_FAILURE);

              for (i = size; i < new_size; i++)
                FD_ZERO (&(open_files_set[i]));

              size = new_size;
            }

          if (fd > open_files_max_fd) {
            open_files_max_fd = fd;
          }

          FD_SET (fd % FD_SETSIZE, &(open_files_set[fd / FD_SETSIZE]));
        }
    }
}
