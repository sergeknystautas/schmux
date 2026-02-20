{
  description = "Project dev environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    rust-overlay.url = "github:oxalica/rust-overlay";
    rust-overlay.inputs.nixpkgs.follows = "nixpkgs";
    llm-agents.url = "github:numtide/llm-agents.nix";
    llm-agents.inputs.nixpkgs.follows = "nixpkgs";
    nur.url = "github:nix-community/NUR";
    nur.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { self, nixpkgs, rust-overlay, llm-agents, nur }: let
    supportedSystems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" "x86_64-darwin" ];
    forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

    # Parse deps.toml
    deps = builtins.fromTOML (builtins.readFile ./deps.toml);

    # =========================================================================
    # Tool Bundles
    # =========================================================================
    # These are pre-defined collections of commonly useful tools.
    # Include them via deps.toml: [bundles] include = ["baseline"]

    # baseline: Modern terminal essentials (28 tools)
    # A curated set of standalone CLI tools (no Python/Perl/Ruby runtimes)
    baselineTools = [
      # Core replacements
      "ripgrep" "fd" "bat" "eza" "sd" "dust" "duf" "bottom" "difftastic"
      # Navigation & search
      "zoxide" "fzf" "broot" "tree"
      # Git
      "delta" "lazygit"
      # Data processing
      "jq" "yq-go" "csvtk" "htmlq" "miller"
      # Shell enhancement
      "atuin" "direnv" "just"
      # Utilities
      "tealdeer" "curlie" "glow" "entr" "pv"
    ];

    # complete: Comprehensive dev environment (61 tools)
    # Everything in baseline plus additional tools for a fully-equipped shell
    completeTools = baselineTools ++ [
      # Additional core replacements
      "procs" "choose"
      # Git & GitHub
      "gh" "git-extras" "tig"
      # Data processing
      "fx"
      # Shell enhancement
      "shellcheck" "starship" "gum"
      # File operations
      "rsync" "trash-cli" "watchexec" "renameutils"
      # Networking
      "curl" "wget" "httpie"
      # Archives
      "unzip" "p7zip" "zstd"
      # System utilities
      "tmux" "watch" "less" "file" "lsof" "moreutils"
      # Development utilities
      "hyperfine" "tokei" "navi"
      # Terminal recording & screenshots
      "vhs" "freeze"
      # Clipboard
      "xclip" "wl-clipboard"
      # Logs
      "lnav"
    ];

    bundles = {
      baseline = baselineTools;
      complete = completeTools;
    };

    # =========================================================================
    # Tool Resolution
    # =========================================================================

    # =========================================================================
    # Rust Toolchain (via rust-overlay)
    # =========================================================================
    # Supports: "stable", "beta", "nightly", or specific versions like "1.75.0"
    # Components configured via rust-components in deps.toml

    getRustToolchain = pkgs: let
      version = deps.tools.rust or null;
      components = deps.rust.components or [ "rustfmt" "clippy" ];

      # Parse version string to rust-bin path
      toolchain =
        if version == "stable" then pkgs.rust-bin.stable.latest.default
        else if version == "beta" then pkgs.rust-bin.beta.latest.default
        else if version == "nightly" then pkgs.rust-bin.nightly.latest.default
        else if version != null then
          # Specific version like "1.75.0"
          pkgs.rust-bin.stable.${version}.default
        else null;

    in
      if toolchain != null then
        toolchain.override { extensions = components; }
      else null;

    # =========================================================================
    # General Tool Resolution
    # =========================================================================

    # Map "tool = version" to nixpkgs package
    mapTool = pkgs: name: version: let

      # Explicit version mappings for tools with non-standard naming
      versionMap = {
        python = {
          "3.10" = pkgs.python310;
          "3.11" = pkgs.python311;
          "3.12" = pkgs.python312;
          "3.13" = pkgs.python313;
        };
        nodejs = {
          "18" = pkgs.nodejs_18;
          "20" = pkgs.nodejs_20;
          "22" = pkgs.nodejs_22;
        };
        go = {
          "1.21" = pkgs.go_1_21;
          "1.22" = pkgs.go_1_22;
          "1.23" = pkgs.go_1_23;
          "1.24" = pkgs.go_1_24;
        };
      };

      # Look up in version map, fall back to direct pkgs lookup
      fromVersionMap = versionMap.${name}.${version} or null;
      fromPkgs = pkgs.${name} or null;

    in
      # Skip rust here - handled separately via getRustToolchain
      if name == "rust" then null
      else if fromVersionMap != null then fromVersionMap
      else if fromPkgs != null then fromPkgs
      else throw "Unknown package: ${name} (version: ${version})";

    # Resolve a tool name to a package (for bundles, always use latest)
    resolveTool = pkgs: name: mapTool pkgs name "*";

    # =========================================================================
    # External Flake Integrations
    # =========================================================================

    # LLM Agents: https://github.com/numtide/llm-agents.nix
    getLlmAgentsPackages = system: let
      agentNames = deps.llm-agents.include or [];
      agentPkgs = llm-agents.packages.${system} or {};
    in
      map (name:
        if builtins.hasAttr name agentPkgs
        then agentPkgs.${name}
        else throw "Unknown llm-agents package: ${name}. See: https://github.com/numtide/llm-agents.nix"
      ) agentNames;

    # NUR (Nix User Repository): https://github.com/nix-community/NUR
    # Format: "repoOwner/packageName" -> nur.repos.repoOwner.packageName
    getNurPackages = pkgs: let
      nurPkgs = import nur { inherit pkgs; nurpkgs = pkgs; };
      packageSpecs = deps.nur.include or [];

      parseSpec = spec: let
        parts = builtins.split "/" spec;
        owner = builtins.elemAt parts 0;
        pkg = builtins.elemAt parts 2;
        repo = nurPkgs.repos.${owner} or null;
      in
        if repo == null then
          throw "Unknown NUR repo: ${owner}. See: https://github.com/nix-community/NUR"
        else if !(builtins.hasAttr pkg repo) then
          throw "Unknown NUR package: ${spec}. Check https://github.com/nix-community/NUR"
        else
          repo.${pkg};
    in
      map parseSpec packageSpecs;

  in {
    devShells = forAllSystems (system: let
      pkgs = import nixpkgs {
        inherit system;
        overlays = [ rust-overlay.overlays.default ];
      };

      # Get included bundles from deps.toml
      includedBundles = deps.bundles.include or [];

      # Collect all tool names from included bundles
      bundleToolNames = builtins.concatLists (
        map (name: bundles.${name} or []) includedBundles
      );

      # Resolve bundle tools to packages
      bundlePackages = map (resolveTool pkgs) bundleToolNames;

      # Map explicit tools from deps.toml (filters out nulls from rust)
      explicitTools = builtins.filter (x: x != null) (
        builtins.attrValues (
          builtins.mapAttrs (mapTool pkgs) (deps.tools or {})
        )
      );

      # Rust toolchain (handled separately for components support)
      rustToolchain = getRustToolchain pkgs;
      rustPackages = if rustToolchain != null then [ rustToolchain ] else [];

      # LLM agents from numtide/llm-agents.nix
      llmAgentsPackages = getLlmAgentsPackages system;

      # NUR packages from nix-community/NUR
      nurPackages = getNurPackages pkgs;

      # Platform-specific: Linux needs libstdc++ for Python native extensions
      linuxDeps = pkgs.lib.optionals pkgs.stdenv.isLinux [
        pkgs.stdenv.cc.cc.lib
      ];

    in {
      default = pkgs.mkShell {
        # Explicit tools first so they take precedence in PATH
        packages = explicitTools ++ rustPackages ++ llmAgentsPackages ++ nurPackages ++ bundlePackages ++ linuxDeps;

        # Linux: ensure native extensions can find libstdc++
        LD_LIBRARY_PATH = pkgs.lib.optionalString pkgs.stdenv.isLinux
          "${pkgs.stdenv.cc.cc.lib}/lib";

        shellHook = let
          llmAgentNames = deps.llm-agents.include or [];
          nurNames = deps.nur.include or [];
        in ''
          echo "Dev environment loaded."
          ${if rustToolchain != null then ''echo "Rust: $(rustc --version)"'' else ""}
          ${if includedBundles != [] then ''echo "Bundles: ${builtins.concatStringsSep ", " includedBundles}"'' else ""}
          ${if llmAgentNames != [] then ''echo "LLM Agents: ${builtins.concatStringsSep ", " llmAgentNames}"'' else ""}
          ${if nurNames != [] then ''echo "NUR: ${builtins.concatStringsSep ", " nurNames}"'' else ""}
        '';
      };
    });
  };
}
