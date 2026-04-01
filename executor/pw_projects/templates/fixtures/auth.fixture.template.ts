/**
 * 认证 Fixture 模板
 *
 * 职责：在 base.fixture 之上叠加认证状态（storageState），
 *       让所有通过 auth.fixture.ts 引入的测试用例自动携带登录态。
 *
 * 使用方式：项目初始化时复制到 projects/<project_key>/fixtures/auth.fixture.ts
 * 注意：此文件为固定模板，LLM 不允许直接编辑。
 */
import path from 'path';
import { test as base } from './base.fixture';

/** 认证状态文件路径 */
const AUTH_STATE_FILE = path.resolve(__dirname, '../auth_states/default.json');

/**
 * 认证测试实例 — 所有需要登录态的测试用例应从此导入 test。
 */
export const test = base.extend({
  storageState: AUTH_STATE_FILE,
});

export { expect } from '@playwright/test';
