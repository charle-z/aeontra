import { parseConsoleData, parseTasksResponse, type ConsoleData, type TasksResponse } from "./dataTypes";

async function getJSON(path: string, signal?: AbortSignal): Promise<unknown> {
  const response = await fetch(path, {
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
    headers: { Accept: "application/json" },
    signal,
  });
  if (response.status === 401) {
    window.location.assign("/console");
    throw new Error("unauthorized");
  }
  if (!response.ok) throw new Error("request failed: " + response.status);
  return response.json();
}

export async function fetchConsoleData(signal?: AbortSignal): Promise<ConsoleData> {
  return parseConsoleData(await getJSON("/console/data", signal));
}

export async function fetchTasks(signal?: AbortSignal): Promise<TasksResponse> {
  return parseTasksResponse(await getJSON("/console/tasks", signal));
}
