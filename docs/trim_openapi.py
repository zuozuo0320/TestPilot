import os
os.chdir(r'd:\hsxa\ai_project\测试管理平台\TestPilot\docs')
fname = '测试管理平台-用例评审模块OpenAPI接口文档-20260404.md'
lines = open(fname, encoding='utf-8').readlines()
print(f"Before: {len(lines)} lines")
open(fname, 'w', encoding='utf-8').writelines(lines[:588])
print(f"After: 588 lines, trimmed {len(lines) - 588} stale lines")
