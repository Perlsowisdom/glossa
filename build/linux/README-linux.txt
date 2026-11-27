Glossa for Linux (64-bit Intel/AMD)
===================================

This folder holds the program (glossa), a launcher entry (io.github.perlsowisdom.glossa.desktop),
the icon (glossa.png, 512 pixels) and the licences. Nothing is installed by unpacking it.

What Glossa needs on your system
--------------------------------
  - GTK 3 and WebKitGTK 4.1, the libraries that draw its window.
      Debian and Ubuntu: sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0
      Fedora:            sudo dnf install gtk3 webkit2gtk4.1
      openSUSE:          sudo zypper install libgtk-3-0 libwebkit2gtk-4_1-0
      Arch:              sudo pacman -S gtk3 webkit2gtk-4.1
  - A glibc as new as Ubuntu 22.04's (2.35) or Debian 12's.
  - Optional: xdg-utils, so Glossa can open links and PDFs in your usual programs.

Run it
------
  ./glossa

Add it to your application menu (optional)
------------------------------------------
  mkdir -p ~/.local/bin ~/.local/share/applications ~/.local/share/icons/hicolor/512x512/apps
  cp glossa ~/.local/bin/
  cp glossa.png ~/.local/share/icons/hicolor/512x512/apps/io.github.perlsowisdom.glossa.png
  sed "s|^Exec=.*|Exec=$HOME/.local/bin/glossa|" io.github.perlsowisdom.glossa.desktop \
    > ~/.local/share/applications/io.github.perlsowisdom.glossa.desktop

Where your library is kept
--------------------------
  $XDG_CONFIG_HOME/Glossa, which is ~/.config/Glossa unless you have set XDG_CONFIG_HOME.
  Glossa never changes it when you update or remove the program.

Updates
-------
  Glossa cannot replace itself. If you turn on update checks, it tells you when a new version is
  out and opens the download page; unpack the new archive over the old program. Your library is kept.

Uninstall
---------
  Delete the program, the launcher entry and the icon you copied. To remove your library as well,
  delete ~/.config/Glossa.

Source, checksums and licences: https://github.com/Perlsowisdom/glossa
