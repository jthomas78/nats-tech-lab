/* The injection key the shell frame publishes itself under. A plugin
   component reads the shell through this and nothing else — there is no
   global, so what a plugin can reach is exactly what the frame provides. */
export const SHELL = Symbol('lab-shell')
