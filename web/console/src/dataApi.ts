import {
  parseConsoleData,
  parseEventLogResponse,
  parseTasksResponse,
  type ConsoleData,
  type EventFilters,
  type EventLogResponse,
  type TaskFilters,
  type TasksResponse,
} from "./dataTypes";

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

function addTaskFilters(query: URLSearchParams, filters: TaskFilters): void {
  if (filters.controller) query.set("controller", filters.controller);
  if (filters.state) query.set("state", filters.state);
  if (filters.operation?.trim()) query.set("operation", filters.operation.trim());
  if (filters.project_id) query.set("project_id", filters.project_id);
  if (filters.edge_id) query.set("edge_id", filters.edge_id);
}

export async function fetchTasks(filters: TaskFilters = {}, cursor = "", signal?: AbortSignal): Promise<TasksResponse> {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor) query.set("cursor", cursor);
  addTaskFilters(query, filters);
  return parseTasksResponse(await getJSON("/console/tasks?" + query.toString(), signal));
}

export async function fetchEventLog(filters: EventFilters = {}, cursor = "", signal?: AbortSignal): Promise<EventLogResponse> {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor) query.set("cursor", cursor);
  if (filters.controller) query.set("controller", filters.controller);
  if (filters.state) query.set("state", filters.state);
  if (filters.event_type) query.set("event_type", filters.event_type);
  if (filters.operation?.trim()) query.set("operation", filters.operation.trim());
  if (filters.project_id) query.set("project_id", filters.project_id);
  if (filters.edge_id) query.set("edge_id", filters.edge_id);
  return parseEventLogResponse(await getJSON("/console/event-log?" + query.toString(), signal));
}
