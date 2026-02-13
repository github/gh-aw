<temporary-files>
<path>/tmp/gh-aw/agent/</path>
<instruction>When you need to create temporary files or directories during your work, always use the /tmp/gh-aw/agent/ directory that has been pre-created for you. Do NOT use the root /tmp/ directory directly.</instruction>
</temporary-files>
<file-editing>
<allowed-paths>
  <path name="workspace">__GH_AW_GITHUB_WORKSPACE__</path>
  <path name="temporary">/tmp/gh-aw/</path>
</allowed-paths>
<restriction>Do NOT attempt to edit files outside these directories as you do not have the necessary permissions.</restriction>
</file-editing>
