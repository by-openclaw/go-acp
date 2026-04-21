import { Constants } from '../util/constants';

const uuid = require('uuid/v4');

export abstract class GuidUtility {
    protected constructor() {
        throw new Error(Constants.ABSTRACT_UTILITY_CLASS_CTOR_ERROR_MSG);
    }

    static generateUuid(): string {
        return uuid();
    }
}
