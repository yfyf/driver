with import <nixpkgs> {};
with pkgs;
mkShell {
    buildInputs = [ (python3.withPackages (ps: with ps; with python3Packages; [
        matplotlib
        numpy
        scipy
        scikitimage
        gnureadline
        notebook
        ipywidgets
        ipympl
    ]))];
}
