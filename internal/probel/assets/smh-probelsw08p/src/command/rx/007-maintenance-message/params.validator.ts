import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { MaintenanceMessageCommandParams } from './params';

/**
 * MaintenanceMessage command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<MaintenanceMessageCommandParams>}
 */
export class CommandParamsValidator implements IValidator<MaintenanceMessageCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {MaintenanceMessageCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: MaintenanceMessageCommandParams) {}

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
        return errors;
    }

    /**
     * Gets a boolean indicating whether the matrixId is out of range
     * + false = matrixId is included within the limits [0-19] || [255]
     * + true = matrixId is out of range
     *
     * @private
     * @returns {boolean} 'true' if the matrixId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        if (this.data.matrixId === 0xff) {
            return false;
        }
        if (this.data.matrixId < 0 || this.data.matrixId > 19) {
            return true;
        }
        return false;
    }

    /**
     * Gets a boolean indicating whether the levelId is out of range
     * + false = levelId is included within the limits [0-15] || [255]
     * + true = levelId is out of range
     *
     * @private
     * @returns {boolean} 'true' if the levelId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        if (this.data.levelId === 0xff) {
            return false;
        }
        if (this.data.levelId < 0 || this.data.levelId > 15) {
            return true;
        }
        return false;
    }
}
