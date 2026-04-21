import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { SingleSourceNamesRequestMessageCommandParams } from './params';

/**
 * SingleSourceNamesRequestMessage command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<SingleSourceNamesRequestMessageCommandParams>}
 */
export class CommandParamsValidator implements IValidator<SingleSourceNamesRequestMessageCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {SingleSourceNamesRequestMessageCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: SingleSourceNamesRequestMessageCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isMatrixIdOutOfRange()) {
            errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isLevelIdOutOfRange()) {
            errors.levelId = cache.getCommandErrorLocaleData(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isSourceIdOutOfRange()) {
            errors.sourceId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     *
     * @private
     * @returns {boolean} 'true' if the matrixId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 255;
    }

    /**
     * Gets a boolean indicating whether the levelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     *
     * @private
     * @returns {boolean} 'true' if the levelId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        return this.data.levelId < 0 || this.data.levelId > 255;
    }

    /**
     * Gets a boolean indicating whether the sourceId is out of range
     * + false = sourceId is included within the limits
     * + true = sourceId  [0-65535] is out of range
     *
     * @private
     * @returns {boolean}  'true' if the sourceId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isSourceIdOutOfRange(): boolean {
        return this.data.sourceId < 0 || this.data.sourceId > 65535;
    }
}
