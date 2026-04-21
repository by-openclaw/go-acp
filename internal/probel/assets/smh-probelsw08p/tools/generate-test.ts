import * as fs from 'fs';
import { CommandIdentifiers, CommandIdentifier } from '../lib/command/command-contract';
import _ from 'lodash';
import * as rimraf from 'rimraf';

const destinationPath = `${__dirname}/../test/generated-test`;
console.log(`> DestinationPath : ${destinationPath}`);

const rxCommandIdentifiers = [
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_INTERROGATE_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.MAINTENANCE_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.DUAL_CONTROLLER_STATUS_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.PROTECT_INTERROGATE_MESSAGE
    // CommandIdentifiers.RX.GENERAL.PROTECT_CONNECT_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.PROTECT_DIS_CONNECT_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.PROTECT_DEVICE_NAME_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.PROTECT_TALLY_DUMP_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.MASTER_PROTECT_CONNECT_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.ALL_SOURCE_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.SINGLE_SOURCE_NAME_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_TIE_LINE_INTERROGATE_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.ALL_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.SINGLE_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.UPDATE_NAME_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_GO_GROUP_SALVO_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE
];

if (fs.existsSync(destinationPath)) {
    rimraf.sync(destinationPath);
}
console.log(`> Creating DestinationPath`);
fs.mkdirSync(destinationPath);

console.log(`> Creating DestinationPath/rx folder...`);
const rxDestinationPath = `${destinationPath}/rx`;
fs.mkdirSync(rxDestinationPath);

const templatePath = `${__dirname}/template/test`;
rxCommandIdentifiers.forEach((commandIdentifier: CommandIdentifier) => {
    const paddedCommandId = _.padStart(commandIdentifier.id.toString(), 3, '0');
    const commandName = commandIdentifier.name.replace('_', '-').toLowerCase();
    const dashCommandName = commandIdentifier.name
        .split('_')
        .map(s => s.toLowerCase())
        .join('-');
    const camelCaseCommandName = commandIdentifier.name
        .split('_')
        .map(s => _.capitalize(s.toLowerCase()))
        .join('')
        .replace('point', 'Point');
    const generatedCommandFolderPath = `${rxDestinationPath}/${paddedCommandId}-${commandName}`;
    console.log(`> Creating DestinationPath/rx/${paddedCommandId}-${commandName} folder...`);
    fs.mkdirSync(generatedCommandFolderPath);
    const rxTxCommandType = commandIdentifier.rxTxType.toLocaleLowerCase();
    const isOptionsAvailable = fs.existsSync(`${__dirname}/../src/command/${rxTxCommandType}/${paddedCommandId}-${dashCommandName}/options.ts`);
    const isParamsAvailable = fs.existsSync(`${__dirname}/../src/command/${rxTxCommandType}/${paddedCommandId}-${dashCommandName}/params.ts`);

    // ----------------------------------------------------------------------
    // command test
    const commandTemplateFilePath = isOptionsAvailable ? `${templatePath}/command.options.test.template.txt` : `${templatePath}/command.test.template.txt`;
    const spaceCommandName = commandIdentifier.name
        .split('_')
        .map(s => _.capitalize(s.toLowerCase()))
        .join(' ')
        .replace('point', 'Point');
    const normalCommandIdentifier = `RX.GENERAL.${commandIdentifier.name}`;
    const extendedCommandIdentifier = `RX.EXTENDED.${commandIdentifier.name}`;

    let commandTestTemplateFileContent = fs.readFileSync(commandTemplateFilePath, { encoding: 'utf-8' });

    commandTestTemplateFileContent = commandTestTemplateFileContent.split('$$SPACE_CMD_NAME$$').join(spaceCommandName);
    commandTestTemplateFileContent = commandTestTemplateFileContent
        .split('$$CAMEL_CMD_NAME$$')
        .join(camelCaseCommandName);
    commandTestTemplateFileContent = commandTestTemplateFileContent.split('$$DASH_CMD_NAME$$').join(dashCommandName);
    commandTestTemplateFileContent = commandTestTemplateFileContent.split('$$CMD_ID$$').join(paddedCommandId);
    commandTestTemplateFileContent = commandTestTemplateFileContent
        .split('$$NORMAL_CMD_IDENTIFER$$')
        .join(normalCommandIdentifier);
    commandTestTemplateFileContent = commandTestTemplateFileContent
        .split('$$EXTENDED_CMD_IDENTIFIER$$')
        .join(extendedCommandIdentifier);

    const generatedCommandTestPath = `${generatedCommandFolderPath}/command.test.ts`;
    fs.writeFileSync(generatedCommandTestPath, commandTestTemplateFileContent);
    console.log(`> Creating ${paddedCommandId}-${commandName}/command.test.ts...`);

    if (isParamsAvailable) {
        // ----------------------------------------------------------------------
        // params validator test
        const paramsValidatorTemplateFilePath = `${templatePath}/params.validator.test.template.txt`;

        let paramsValidatorTestTemplateFileContent = fs.readFileSync(paramsValidatorTemplateFilePath, {
            encoding: 'utf-8'
        });

        paramsValidatorTestTemplateFileContent = paramsValidatorTestTemplateFileContent
            .split('$$SPACE_CMD_NAME$$')
            .join(spaceCommandName);
        paramsValidatorTestTemplateFileContent = paramsValidatorTestTemplateFileContent
            .split('$$CAMEL_CMD_NAME$$')
            .join(camelCaseCommandName);
        paramsValidatorTestTemplateFileContent = paramsValidatorTestTemplateFileContent
            .split('$$DASH_CMD_NAME$$')
            .join(dashCommandName);
        paramsValidatorTestTemplateFileContent = paramsValidatorTestTemplateFileContent
            .split('$$CMD_ID$$')
            .join(paddedCommandId);
        paramsValidatorTestTemplateFileContent = paramsValidatorTestTemplateFileContent
            .split('$$NORMAL_CMD_IDENTIFER$$')
            .join(normalCommandIdentifier);
        paramsValidatorTestTemplateFileContent = paramsValidatorTestTemplateFileContent
            .split('$$EXTENDED_CMD_IDENTIFIER$$')
            .join(extendedCommandIdentifier);

        const generatedParamsValidatorTestPath = `${generatedCommandFolderPath}/params.validator.test.ts`;
        fs.writeFileSync(generatedParamsValidatorTestPath, paramsValidatorTestTemplateFileContent);
        console.log(`> Creating ${paddedCommandId}-${commandName}/params.validator.test.ts...`);
    }

    if (isOptionsAvailable) {
        // ----------------------------------------------------------------------
        // options validator test
        const optionsValidatorTemplateFilePath = `${templatePath}/options.validator.test.template.txt`;

        let optionsValidatorTestTemplateFileContent = fs.readFileSync(optionsValidatorTemplateFilePath, {
            encoding: 'utf-8'
        });

        optionsValidatorTestTemplateFileContent = optionsValidatorTestTemplateFileContent
            .split('$$SPACE_CMD_NAME$$')
            .join(spaceCommandName);
        optionsValidatorTestTemplateFileContent = optionsValidatorTestTemplateFileContent
            .split('$$CAMEL_CMD_NAME$$')
            .join(camelCaseCommandName);
        optionsValidatorTestTemplateFileContent = optionsValidatorTestTemplateFileContent
            .split('$$DASH_CMD_NAME$$')
            .join(dashCommandName);
        optionsValidatorTestTemplateFileContent = optionsValidatorTestTemplateFileContent
            .split('$$CMD_ID$$')
            .join(paddedCommandId);
        optionsValidatorTestTemplateFileContent = optionsValidatorTestTemplateFileContent
            .split('$$NORMAL_CMD_IDENTIFER$$')
            .join(normalCommandIdentifier);
        optionsValidatorTestTemplateFileContent = optionsValidatorTestTemplateFileContent
            .split('$$EXTENDED_CMD_IDENTIFIER$$')
            .join(extendedCommandIdentifier);

        const generatedOptionsValidatorTestPath = `${generatedCommandFolderPath}/options.validator.test.ts`;
        fs.writeFileSync(generatedOptionsValidatorTestPath, optionsValidatorTestTemplateFileContent);
        console.log(`> Creating ${paddedCommandId}-${commandName}/options.validator.test.ts...`);
    }
});

console.log(`> Creating DestinationPath/tx folder...`);
const txDestinationPath = `${destinationPath}/tx`;
fs.mkdirSync(txDestinationPath);
