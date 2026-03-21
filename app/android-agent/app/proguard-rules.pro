# Keep Rhino JavaScript engine internals — R8 would otherwise strip reflectively
# accessed interpreter classes, causing runtime ClassNotFoundException.
-keep class org.mozilla.javascript.** { *; }
