import { fs } from 'mz';

import { Maybe } from '../util/type';
import { IIdentifiable } from './identifiable';

export interface IDataService<TData extends IIdentifiable> {
    addAsync(data: TData, autoSave: boolean): Promise<TData>;
    deleteAsync(data: TData, autoSave: boolean): Promise<void>;
    getById(id: string): Maybe<TData>;
    getAll(): Array<TData>;
    saveAsync(): Promise<void>;
    loadAsync(): Promise<void>;
}

const errors = {
    FILE_NOT_FOUND: (filePath: string): Error => new Error(`File [${filePath}] does not exist !`),
    SAVE_FAILURE: (err: Error): Error => new Error(`Unable to save the data to json file due to [${err}] !`),
    LOAD_FAILURE: (err: Error): Error => new Error(`Unable to load the data from json file due to [${err}] !`),
    DATA_ALREADY_EXIST: (id: string): Error => new Error(`Data already exists with the same key [${id}] !`),
    DATA_NOT_FOUND: (id: string): Error => new Error(`Data not found for this key [${id}] !`)
};

export class JsonDataService<TData extends IIdentifiable> implements IDataService<TData> {
    private static readonly ENCODING = 'UTF8';
    private static readonly AUTO_SAVE = false;

    private _data: Map<string, TData> = new Map<string, TData>();

    constructor(private _jsonFilePath: string) {
        if (!fs.exists(_jsonFilePath)) {
            throw errors.FILE_NOT_FOUND(_jsonFilePath);
        }
    }

    async addAsync(data: TData, autoSave: boolean = JsonDataService.AUTO_SAVE): Promise<TData> {
        const dataId = data.id;
        if (this._data.has(dataId)) {
            throw errors.DATA_ALREADY_EXIST(data.id);
        }

        this._data.set(dataId, data);
        if (autoSave) {
            await this.saveAsync();
        }
        return data;
    }

    async updateAsync(data: TData, autoSave: boolean = JsonDataService.AUTO_SAVE): Promise<TData> {
        const dataId = data.id;
        if (!this._data.has(dataId)) {
            throw errors.DATA_NOT_FOUND(dataId);
        }

        this._data.set(dataId, data);
        if (autoSave) {
            await this.saveAsync();
        }
        return data;
    }

    async deleteAsync(data: TData, autoSave: boolean = JsonDataService.AUTO_SAVE): Promise<void> {
        const dataId = data.id;
        if (!this._data.has(dataId)) {
            throw errors.DATA_NOT_FOUND(dataId);
        }
        this._data.delete(dataId);
        if (autoSave) {
            await this.saveAsync();
        }
    }

    getById(id: string): Maybe<TData> {
        return this._data.has(id) ? this._data.get(id) : undefined;
    }

    getAll(): Array<TData> {
        return Array.from(this._data).map(([key, value]) => value);
    }

    saveAsync(): Promise<void> {
        try {
            const content = JSON.stringify(this.getAll());
            return fs.writeFile(this._jsonFilePath, content, { encoding: JsonDataService.ENCODING });
        } catch (err) {
            throw errors.SAVE_FAILURE(err);
        }
    }

    async loadAsync(): Promise<void> {
        try {
            const fileContent: string = await fs.readFile(this._jsonFilePath, { encoding: JsonDataService.ENCODING });
            this._data = new Map<string, TData>();
            const dataList = JSON.parse(fileContent);
            if (dataList) {
                dataList.forEach((data: TData) => this._data.set(data.id, data));
            }
        } catch (err) {
            throw errors.LOAD_FAILURE(err);
        }
    }
}
