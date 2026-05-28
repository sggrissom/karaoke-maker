import * as rpc from "vlens/rpc"

export interface SubmitJobRequest {
    URL: string
}

export interface SubmitJobResponse {
    JobID: string
}

export interface GetJobRequest {
    JobID: string
}

export interface Job {
    ID: string
    URL: string
    Status: string
    Step: string
    Progress: number
    Title: string
    CreatedAt: string
    CompletedAt: string
    Error: string
    StepStartedAt: string
    BPM: number
    Key: string
}

export interface Empty {
}

export interface ListJobsResponse {
    Jobs: Job[]
}

export interface DeleteJobRequest {
    JobID: string
}

export async function SubmitJob(data: SubmitJobRequest): Promise<rpc.Response<SubmitJobResponse>> {
    return await rpc.call<SubmitJobResponse>('SubmitJob', JSON.stringify(data));
}

export async function GetJob(data: GetJobRequest): Promise<rpc.Response<Job>> {
    return await rpc.call<Job>('GetJob', JSON.stringify(data));
}

export async function ListJobs(data: Empty): Promise<rpc.Response<ListJobsResponse>> {
    return await rpc.call<ListJobsResponse>('ListJobs', JSON.stringify(data));
}

export async function DeleteJob(data: DeleteJobRequest): Promise<rpc.Response<Empty>> {
    return await rpc.call<Empty>('DeleteJob', JSON.stringify(data));
}

