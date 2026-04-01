/**
 * merger.ts — AST 合并器
 * 
 * 功能：根据 LLM 输出的 page_updates 增量指令，
 * 通过 ts-morph 对已有 Page Object 文件进行 AST 级别的安全合并。
 * 
 * 支持的操作：
 *   - append_locator:  追加新的 readonly locator 声明
 *   - append_action:   追加新的 action 方法
 *   - extend_action_non_breaking: 替换已有方法体（非破坏性扩展）
 * 
 * 使用：
 *   echo '<json>' | npx tsx src/merger.ts
 *   或
 *   npx tsx src/merger.ts --input <file.json>
 */

import { Project, SyntaxKind, ClassDeclaration, SourceFile, PropertyDeclaration } from "ts-morph";
import * as fs from "fs";
import * as path from "path";
import * as crypto from "crypto";

// ── 输入/输出类型 ──

interface MergeInput {
  workspace_root: string;
  updates: PageUpdate[];
}

interface PageUpdate {
  page_name: string;
  file_path: string;   // 相对于 workspace_root 的路径
  operation: string;    // append_locator | append_action | extend_action_non_breaking
  new_locators?: NewLocator[];
  new_actions?: NewAction[];
  extend_actions?: ExtendAction[];
}

interface NewLocator {
  name: string;
  definition: string;  // 如 page.getByRole('button', { name: '提交' })
  summary: string;     // 中文说明
}

interface NewAction {
  name: string;
  summary: string;
  params_signature: string;
  uses_locators: string[];
  content: string;     // 完整方法体代码（含 JSDoc）
}

interface ExtendAction {
  name: string;
  change_type: string;
  summary: string;
  content: string;     // 修改后的完整方法体代码
}

interface MergeResult {
  success: boolean;
  merged_files: MergedFile[];
  errors: string[];
  warnings: string[];
}

interface MergedFile {
  file_path: string;
  operations_applied: string[];
  content: string;
  content_hash: string;
}

// ── 主逻辑 ──

function main(): void {
  let inputJson: string;

  // 支持 --input <file> 和 stdin 两种输入方式
  const inputArgIdx = process.argv.indexOf("--input");
  if (inputArgIdx !== -1 && process.argv[inputArgIdx + 1]) {
    inputJson = fs.readFileSync(process.argv[inputArgIdx + 1], "utf-8");
  } else {
    inputJson = fs.readFileSync(0, "utf-8"); // stdin
  }

  const input: MergeInput = JSON.parse(inputJson);
  const result = runMerge(input);
  
  // 输出 JSON 结果到 stdout
  process.stdout.write(JSON.stringify(result, null, 2));
}

function runMerge(input: MergeInput): MergeResult {
  const result: MergeResult = {
    success: true,
    merged_files: [],
    errors: [],
    warnings: [],
  };

  const project = new Project({
    useInMemoryFileSystem: false,
    compilerOptions: {
      strict: true,
      target: 99, // ESNext
    },
  });

  for (const update of input.updates) {
    try {
      const absolutePath = path.resolve(input.workspace_root, update.file_path);

      if (!fs.existsSync(absolutePath)) {
        result.errors.push(`文件不存在: ${update.file_path}`);
        result.success = false;
        continue;
      }

      const sourceFile = project.addSourceFileAtPath(absolutePath);
      const classDecl = findPageClass(sourceFile, update.page_name);

      if (!classDecl) {
        result.errors.push(`未找到类 ${update.page_name} in ${update.file_path}`);
        result.success = false;
        continue;
      }

      const operations: string[] = [];

      // 追加 locator
      if (update.new_locators && update.new_locators.length > 0) {
        for (const loc of update.new_locators) {
          const merged = appendLocator(classDecl, loc);
          if (merged) {
            operations.push(`append_locator:${loc.name}`);
          } else {
            result.warnings.push(`Locator ${loc.name} 已存在于 ${update.page_name}，跳过`);
          }
        }

        // 更新 constructor 中的 locator 初始化
        updateConstructorLocators(classDecl, update.new_locators);
      }

      // 追加 action
      if (update.new_actions && update.new_actions.length > 0) {
        for (const action of update.new_actions) {
          const merged = appendAction(classDecl, action);
          if (merged) {
            operations.push(`append_action:${action.name}`);
          } else {
            result.warnings.push(`Action ${action.name} 已存在于 ${update.page_name}，跳过`);
          }
        }
      }

      // 扩展已有 action（非破坏性）
      if (update.extend_actions && update.extend_actions.length > 0) {
        for (const ext of update.extend_actions) {
          const extended = extendAction(classDecl, ext);
          if (extended) {
            operations.push(`extend_action:${ext.name}`);
          } else {
            result.errors.push(`无法扩展方法 ${ext.name}：方法不存在于 ${update.page_name}`);
          }
        }
      }

      if (operations.length > 0) {
        // 保存修改后的文件
        sourceFile.saveSync();
        const content = sourceFile.getFullText();

        result.merged_files.push({
          file_path: update.file_path,
          operations_applied: operations,
          content: content,
          content_hash: simpleHash(content),
        });
      }
    } catch (err: any) {
      result.errors.push(`处理 ${update.page_name} 时异常: ${err.message}`);
      result.success = false;
    }
  }

  return result;
}

// ── AST 操作 ──

function findPageClass(sourceFile: SourceFile, className: string): ClassDeclaration | undefined {
  return sourceFile.getClasses().find((c: ClassDeclaration) => c.getName() === className);
}

function appendLocator(classDecl: ClassDeclaration, locator: NewLocator): boolean {
  // 检查是否已存在同名属性
  const existing = classDecl.getProperty(locator.name);
  if (existing) {
    return false;
  }

  // 找到最后一个 readonly Locator 属性的位置，在其后插入
  const properties = classDecl.getProperties();
  const locatorProps = properties.filter((p: PropertyDeclaration) => {
    const typeNode = p.getTypeNode();
    return typeNode && typeNode.getText() === "Locator";
  });

  const insertIndex = locatorProps.length > 0
    ? locatorProps[locatorProps.length - 1].getChildIndex() + 1
    : getConstructorIndex(classDecl);

  classDecl.insertProperty(insertIndex, {
    name: locator.name,
    isReadonly: true,
    type: "Locator",
    docs: [{ description: locator.summary }],
  });

  return true;
}

function updateConstructorLocators(classDecl: ClassDeclaration, newLocators: NewLocator[]): void {
  const constructor = classDecl.getConstructors()[0];
  if (!constructor) return;

  const body = constructor.getBody();
  if (!body) return;

  // 在 constructor body 末尾添加 locator 初始化
  for (const loc of newLocators) {
    // 检查是否已有此 locator 的初始化
    const bodyText = body.getText();
    if (bodyText.includes(`this.${loc.name}`)) {
      continue;
    }

    constructor.addStatements(`this.${loc.name} = ${loc.definition};`);
  }
}

function appendAction(classDecl: ClassDeclaration, action: NewAction): boolean {
  // 检查是否已存在同名方法
  const existing = classDecl.getMethod(action.name);
  if (existing) {
    return false;
  }

  // 直接追加方法代码到类末尾
  classDecl.addMember(action.content);

  return true;
}

function extendAction(classDecl: ClassDeclaration, ext: ExtendAction): boolean {
  const method = classDecl.getMethod(ext.name);
  if (!method) {
    return false;
  }

  // 获取方法在类中的索引
  const index = method.getChildIndex();

  // 移除旧方法
  method.remove();

  // 在相同位置插入新的方法代码
  classDecl.insertMember(index, ext.content);

  return true;
}

function getConstructorIndex(classDecl: ClassDeclaration): number {
  const constructor = classDecl.getConstructors()[0];
  if (constructor) {
    return constructor.getChildIndex();
  }
  // 如果没有 constructor，在第一个属性之后
  const props = classDecl.getProperties();
  if (props.length > 0) {
    return props[props.length - 1].getChildIndex() + 1;
  }
  return 0;
}

function simpleHash(content: string): string {
  return crypto.createHash("sha256").update(content).digest("hex");
}

// ── 入口 ──
main();
